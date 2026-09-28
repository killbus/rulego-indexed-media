package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestInitialProbeRequiresProvenEOFAndExactBody(t *testing.T) {
	for _, test := range []struct {
		name, contentRange, encoding, kind string
		status, length, calls              int
	}{
		{name: "short EOF", contentRange: "bytes 0-3/4", length: 4, calls: 1},
		{name: "exact", contentRange: "bytes 0-65535/99999", length: 65536, calls: 1},
		{name: "exact EOF", contentRange: "bytes 0-65535/65536", length: 65536, calls: 1},
		{name: "exact unknown total", contentRange: "bytes 0-65535/*", length: 65536, calls: 1},
		{name: "unknown short total", contentRange: "bytes 0-3/*", length: 4, kind: "invalid_source", calls: 1},
		{name: "not EOF", contentRange: "bytes 0-3/5", length: 4, kind: "invalid_source", calls: 1},
		{name: "shifted", contentRange: "bytes 1-3/4", length: 3, kind: "invalid_source", calls: 1},
		{name: "zero total", contentRange: "bytes 0-3/0", length: 4, kind: "invalid_source", calls: 1},
		{name: "invalid total", contentRange: "bytes 0-3/no", length: 4, kind: "invalid_source", calls: 1},
		{name: "trailing junk", contentRange: "bytes 0-3/4 junk", length: 4, kind: "invalid_source", calls: 1},
		{name: "total overflow", contentRange: "bytes 0-3/18446744073709551616", length: 4, kind: "invalid_source", calls: 1},
		{name: "inconsistent exact", contentRange: "bytes 0-65535/4", length: 65536, kind: "invalid_source", calls: 1},
		{name: "short body", contentRange: "bytes 0-3/4", length: 3, kind: "source_unavailable", calls: 3},
		{name: "extra body", contentRange: "bytes 0-3/4", length: 5, kind: "source_unavailable", calls: 3},
		{name: "unproven short body", contentRange: "bytes 0-65535/99999", length: 4, kind: "source_unavailable", calls: 3},
		{name: "over cap", contentRange: "bytes 0-65535/99999", length: 65537, kind: "source_limit", calls: 1},
		{name: "encoded", contentRange: "bytes 0-3/4", encoding: "gzip", length: 4, kind: "invalid_source", calls: 1},
		{name: "ignored range", status: 200, contentRange: "bytes 0-3/4", length: 4, kind: "invalid_source", calls: 1},
		{name: "stale", status: 403, kind: "source_stale", calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := bareManager()
			defer manager.Close()
			calls := 0
			manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Header.Get("Range") != "bytes=0-65535" || request.Header.Get("Accept-Encoding") != "identity" {
					t.Errorf("probe headers=%v", request.Header)
				}
				status := test.status
				if status == 0 {
					status = http.StatusPartialContent
				}
				return &http.Response{StatusCode: status,
					Header: http.Header{"Content-Range": []string{test.contentRange}, "Content-Encoding": []string{test.encoding}},
					Body:   io.NopCloser(strings.NewReader(strings.Repeat("x", test.length)))}, nil
			})}
			representation := mediaRepresentation{URL: "https://media.invalid/audio", Headers: map[string]string{"Range": "bytes=0-9999999", "Accept-Encoding": "gzip"}}
			body, err := manager.fetchIndexProbe(context.Background(), representation)
			if test.kind == "" {
				if err != nil || len(body) != test.length {
					t.Fatalf("length=%d err=%v", len(body), err)
				}
			} else if asProblem(err).kind != test.kind {
				t.Fatalf("error=%v want=%s", err, test.kind)
			}
			if calls != test.calls {
				t.Fatalf("requests=%d want=%d", calls, test.calls)
			}
		})
	}
}

func TestProbeRetriesAndLaterRangesRemainExact(t *testing.T) {
	manager := bareManager()
	defer manager.Close()
	calls := 0
	manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls <= 2 {
			return response(http.StatusServiceUnavailable, ""), nil
		}
		return response(http.StatusPartialContent, "data"), nil
	})}
	representation := mediaRepresentation{URL: "https://media.invalid/audio"}
	if body, err := manager.fetchIndexProbe(context.Background(), representation); err != nil || string(body) != "data" || calls != 3 {
		t.Fatalf("body=%q err=%v calls=%d", body, err, calls)
	}
	for _, length := range []uint64{16, initialIndexProbeBytes} {
		if _, err := manager.fetchRange(context.Background(), representation, 0, length); asProblem(err).kind != "invalid_source" {
			t.Fatalf("exact index range %d accepted clipped EOF: %v", length, err)
		}
	}
	file, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := manager.copySourceRange(context.Background(), file, representation, 0, 8); asProblem(err).kind != "source_unavailable" {
		t.Fatalf("production accepted clipped EOF: %v", err)
	}
}

func TestSIDXBeyondProbeUsesExactBoundedReads(t *testing.T) {
	manager := bareManager()
	defer manager.Close()
	data := make([]byte, initialIndexProbeBytes)
	binary.BigEndian.PutUint32(data[:4], initialIndexProbeBytes)
	copy(data[4:8], "free")
	raw := makeSIDX(48000, 0, 0, testReference{size: 3, duration: 48000})
	data = append(data, raw...)
	data = append(data, 'A', 'A', 'A')
	var ranges []string
	manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		ranges = append(ranges, request.Header.Get("Range"))
		return fixtureResponse(request, data), nil
	})}
	index, err := manager.discoverIndex(context.Background(), mediaRepresentation{URL: "https://media.invalid/audio"}, false)
	if err != nil || index.InitSize != initialIndexProbeBytes || len(index.Refs) != 1 {
		t.Fatalf("index=%+v err=%v", index, err)
	}
	want := []string{"bytes=0-65535", "bytes=65536-65551", fmt.Sprintf("bytes=65536-%d", initialIndexProbeBytes+len(raw)-1)}
	if strings.Join(ranges, ",") != strings.Join(want, ",") {
		t.Fatalf("ranges=%v want=%v", ranges, want)
	}
}

func TestDiscoveryAppliesSAPOnlyToSelectedVideo(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		manager := bareManager()
		manager.inspectFn = manager.inspectSource
		manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			// All references are direct audio references without video SAP flags.
			return fixtureResponse(request, indexedFixture(false)), nil
		})}
		_, err := manager.Inspect(context.Background(), inspectRequest{Source: selectedLease(mode)})
		manager.Close()
		if mode == "audio" && err != nil || mode != "audio" && asProblem(err).kind != "invalid_source" {
			t.Fatalf("mode=%s err=%v", mode, err)
		}
	}
}

func TestRangeRejectsAmbiguousOrDecodedHeaders(t *testing.T) {
	for _, alter := range []func(*http.Response){
		func(r *http.Response) { r.Header.Add("Content-Range", "bytes 0-3/5") },
		func(r *http.Response) { r.Header["Content-Encoding"] = []string{"identity", "gzip"} },
		func(r *http.Response) { r.Uncompressed = true },
	} {
		r := response(http.StatusPartialContent, "data")
		alter(r)
		if validRangeResponse(r, 0, 3) {
			t.Fatalf("ambiguous/decoded range accepted: %+v", r)
		}
		_ = r.Body.Close()
	}
}
