package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLease(videoURL, audioURL string) mediaLease {
	return mediaLease{
		SourceKey: "provider:asset",
		Video: mediaRepresentation{
			URL: videoURL, Headers: map[string]string{"Authorization": "video-lease"},
			Container: "mp4", Codec: "avc1.64002a",
		},
		Audio: mediaRepresentation{
			URL: audioURL, Headers: map[string]string{"Authorization": "audio-lease"},
			Container: "m4a", Codec: "mp4a.40.2",
		},
	}
}

func testBundle(source mediaLease, revision string) *sourceBundle {
	videoIndex := mediaIndex{Timescale: 1000, Raw: []byte("video-index"), InitSize: 8, Refs: []mediaReference{{Offset: 8, Size: 3, Duration: 2000, Start: 0}}}
	audioIndex := mediaIndex{Timescale: 1000, Raw: []byte("audio-index"), InitSize: 8, Refs: []mediaReference{{Offset: 8, Size: 3, Duration: 2000, Start: 0}}}
	return &sourceBundle{source: source, revision: revision, videoIdx: videoIndex, audioIdx: audioIndex}
}

func bareManager() *sourceManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &sourceManager{
		config: nodeConfiguration{IndexTimeoutMs: 1000, ProduceTimeoutMs: 1000},
		ctx:    ctx, cancel: cancel, client: http.DefaultClient,
		bundles:  make(map[string]*sourceBundle),
		inflight: make(map[string]*inspectCall),
	}
}

func TestConcurrentColdInspectSingleflightsIndexDiscovery(t *testing.T) {
	source := testLease("https://media.invalid/video", "https://media.invalid/audio")
	manager := bareManager()
	defer manager.Close()
	var calls atomic.Int32
	release := make(chan struct{})
	manager.inspectFn = func(context.Context, mediaLease) (*sourceBundle, error) {
		calls.Add(1)
		<-release
		return testBundle(source, strings.Repeat("a", 64)), nil
	}
	const workers = 16
	results := make(chan inspectResult, workers)
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := manager.Inspect(context.Background(), inspectRequest{Source: source})
			results <- result
			errors <- err
		}()
	}
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wait.Wait()
	close(results)
	close(errors)
	if calls.Load() != 1 {
		t.Fatalf("index discoveries = %d", calls.Load())
	}
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.Revision != strings.Repeat("a", 64) || len(result.Segments) != 1 {
			t.Fatalf("result = %+v", result)
		}
	}
}

func TestIndexCacheIsBoundToTheCompleteLease(t *testing.T) {
	first := testLease("https://media.invalid/video?lease=one", "https://media.invalid/audio?lease=one")
	second := testLease("https://media.invalid/video?lease=two", "https://media.invalid/audio?lease=two")
	third := second
	third.Video.Headers = map[string]string{"Authorization": "refreshed-video-lease"}
	manager := bareManager()
	defer manager.Close()
	var calls atomic.Int32
	manager.inspectFn = func(_ context.Context, source mediaLease) (*sourceBundle, error) {
		calls.Add(1)
		return testBundle(source, strings.Repeat("a", 64)), nil
	}
	for _, source := range []mediaLease{first, first, second, second, third, third} {
		bundle, err := manager.getBundle(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		if bundle.source.Video.URL != source.Video.URL || bundle.source.Audio.URL != source.Audio.URL {
			t.Fatalf("cached stale lease: %#v", bundle.source)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("index discoveries = %d, want 3", calls.Load())
	}
}

func TestCloseCancelsInspection(t *testing.T) {
	manager := bareManager()
	manager.inspectFn = func(ctx context.Context, _ mediaLease) (*sourceBundle, error) {
		<-ctx.Done()
		return nil, contextProblem(ctx.Err())
	}
	done := make(chan error, 1)
	go func() {
		_, err := manager.Inspect(context.Background(), inspectRequest{Source: testLease("https://media.invalid/video", "https://media.invalid/audio")})
		done <- err
	}()
	manager.Close()
	if failure := asProblem(<-done); failure.kind != "canceled" {
		t.Fatalf("error = %#v", failure)
	}
}

func TestRevisionExcludesLeaseURLsAndHeaders(t *testing.T) {
	index := mediaIndex{Raw: []byte("immutable-index"), Timescale: 1000, InitSize: 40}
	first := testLease("https://lease.invalid/video?sig=one", "https://lease.invalid/audio?sig=one")
	second := testLease("https://lease.invalid/v2?sig=two", "https://lease.invalid/a2?sig=two")
	second.Video.Headers = map[string]string{"Authorization": "refreshed"}
	second.Audio.Headers = map[string]string{"Authorization": "refreshed"}
	if sourceRevision(first, index, index) != sourceRevision(second, index, index) {
		t.Fatal("transient URL or headers affected revision")
	}
	second.SourceKey = "provider:other"
	if sourceRevision(first, index, index) == sourceRevision(second, index, index) {
		t.Fatal("stable source identity did not affect revision")
	}
	second = first
	second.Video.Codec = "avc1.4d401f"
	if sourceRevision(first, index, index) == sourceRevision(second, index, index) {
		t.Fatal("immutable media facts did not affect revision")
	}
	changedIndex := index
	changedIndex.Raw = []byte("changed-index")
	if sourceRevision(first, index, index) == sourceRevision(first, changedIndex, index) {
		t.Fatal("changed media index did not affect revision")
	}
}

func TestSourceRevisionFixedDigest(t *testing.T) {
	source := mediaLease{
		SourceKey: "fixture:paired-index-v1",
		Video:     mediaRepresentation{Container: "MP4", Codec: "AVC1.640028"},
		Audio:     mediaRepresentation{Container: "M4A", Codec: "MP4A.40.2"},
	}
	video := mediaIndex{
		InitSize: 0x0102030405060708,
		Raw:      []byte{0x00, 0x76, 0x69, 0x64, 0x65, 0x6f, 0xff},
	}
	audio := mediaIndex{
		InitSize: 0x1112131415161718,
		Raw:      []byte{0x80, 0x61, 0x75, 0x64, 0x69, 0x6f, 0x00, 0xfe},
	}
	// Pin the hash framing and track order from pre-rename commit ab2217bccb46.
	const want = "d6e815a371172793a89d537156d03db8394870361c9ac6624ff3510a1ed657f8"
	if got := sourceRevision(source, video, audio); got != want {
		t.Fatalf("source revision = %q, want %q", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

type disconnectReader struct{ read bool }

func (reader *disconnectReader) Read(buffer []byte) (int, error) {
	if reader.read {
		return 0, io.ErrUnexpectedEOF
	}
	reader.read = true
	buffer[0] = 'x'
	return 1, nil
}

func TestRangeRetriesTransientFailures(t *testing.T) {
	tests := []struct {
		name     string
		response func(int) (*http.Response, error)
	}{
		{name: "429", response: func(attempt int) (*http.Response, error) {
			if attempt < 3 {
				return response(http.StatusTooManyRequests, ""), nil
			}
			return response(http.StatusPartialContent, "data"), nil
		}},
		{name: "5xx", response: func(attempt int) (*http.Response, error) {
			if attempt < 3 {
				return response(http.StatusServiceUnavailable, ""), nil
			}
			return response(http.StatusPartialContent, "data"), nil
		}},
		{name: "network", response: func(attempt int) (*http.Response, error) {
			if attempt < 3 {
				return nil, errors.New("temporary connection failure")
			}
			return response(http.StatusPartialContent, "data"), nil
		}},
		{name: "disconnect", response: func(attempt int) (*http.Response, error) {
			if attempt < 3 {
				return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{"bytes 0-3/4"}}, Body: io.NopCloser(&disconnectReader{})}, nil
			}
			return response(http.StatusPartialContent, "data"), nil
		}},
		{name: "clean early EOF", response: func(attempt int) (*http.Response, error) {
			if attempt < 3 {
				return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Range": []string{"bytes 0-3/4"}}, Body: io.NopCloser(strings.NewReader("x"))}, nil
			}
			return response(http.StatusPartialContent, "data"), nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := bareManager()
			defer manager.Close()
			var calls atomic.Int32
			manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return test.response(int(calls.Add(1)))
			})}
			body, err := manager.fetchRange(context.Background(), mediaRepresentation{URL: "https://media.invalid/video"}, 0, 4)
			if err != nil || string(body) != "data" || calls.Load() != 3 {
				t.Fatalf("body=%q calls=%d err=%v", body, calls.Load(), err)
			}
		})
	}
}

func TestRangeReportsStaleLeaseWithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone} {
		manager := bareManager()
		var calls atomic.Int32
		manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return response(status, ""), nil
		})}
		_, err := manager.fetchRange(context.Background(), mediaRepresentation{URL: "https://media.invalid/video"}, 0, 4)
		manager.Close()
		if failure := asProblem(err); failure.kind != "source_stale" || calls.Load() != 1 {
			t.Fatalf("status=%d calls=%d error=%#v", status, calls.Load(), err)
		}
	}
}

func TestRangeRejectsServerIgnoringRange(t *testing.T) {
	manager := bareManager()
	defer manager.Close()
	manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, "data"), nil
	})}
	_, err := manager.fetchRange(context.Background(), mediaRepresentation{URL: "https://media.invalid/video"}, 0, 4)
	if failure := asProblem(err); failure.kind != "invalid_source" {
		t.Fatalf("error = %#v", err)
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Range": []string{fmt.Sprintf("bytes 0-%d/%d", len(body)-1, len(body))}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestStrictGenericRequestDecoding(t *testing.T) {
	valid := `{"operation":"inspect","source":{"sourceKey":"provider:asset","video":{"url":"https://media.invalid/v","headers":{},"container":"mp4","codec":"avc1.64002a"},"audio":{"url":"https://media.invalid/a","headers":{},"container":"m4a","codec":"mp4a.40.2"}}}`
	if _, _, err := decodeRequest(valid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeRequest(strings.TrimSuffix(valid, "}") + `,"videoId":"old-contract"}`); err == nil {
		t.Fatal("old provider-specific field succeeded")
	}
	if _, _, err := decodeRequest(strings.Replace(valid, "avc1.64002a", "vp9", 1)); err == nil {
		t.Fatal("incompatible video codec succeeded")
	}
	for _, operation := range []string{"inspect", "produce"} {
		for _, missing := range []string{"", "video", "audio"} {
			t.Run(operation+"/missing-"+missing, func(t *testing.T) {
				var request map[string]any
				if err := json.Unmarshal([]byte(valid), &request); err != nil {
					t.Fatal(err)
				}
				request["operation"] = operation
				if operation == "produce" {
					request["expectedRevision"] = strings.Repeat("a", 64)
					request["segment"] = 0
					request["stagingDir"] = t.TempDir()
					request["maxBytes"] = 1024
					request["publishBy"] = time.Now().Add(time.Hour)
				}
				if missing != "" {
					delete(request["source"].(map[string]any), missing)
				}
				body, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				gotOperation, _, err := decodeRequest(string(body))
				if missing == "" {
					if err != nil || gotOperation != operation {
						t.Fatalf("paired request operation=%q err=%v", gotOperation, err)
					}
				} else if err == nil {
					t.Fatalf("missing %s succeeded", missing)
				}
			})
		}
	}
}
