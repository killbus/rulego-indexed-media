package main

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestParseRange(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		size        int64
		wantStart   int64
		wantEnd     int64
		wantFailure bool
	}{
		{name: "closed", value: "bytes=2-7", size: 8, wantStart: 2, wantEnd: 7},
		{name: "missing", size: 8, wantFailure: true},
		{name: "open ended", value: "bytes=2-", size: 8, wantFailure: true},
		{name: "suffix", value: "bytes=-2", size: 8, wantFailure: true},
		{name: "multiple", value: "bytes=0-1,4-5", size: 8, wantFailure: true},
		{name: "signed number", value: "bytes=+2-7", size: 8, wantFailure: true},
		{name: "clips at EOF", value: "bytes=2-8", size: 8, wantStart: 2, wantEnd: 7},
		{name: "short initial probe", value: "bytes=0-65535", size: 8, wantStart: 0, wantEnd: 7},
		{name: "starts at EOF", value: "bytes=8-9", size: 8, wantFailure: true},
		{name: "empty file", value: "bytes=0-65535", size: 0, wantFailure: true},
		{name: "overflow", value: "bytes=0-9223372036854775808", size: 8, wantFailure: true},
		{name: "reversed", value: "bytes=7-2", size: 8, wantFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end, err := parseRange(test.value, test.size)
			if test.wantFailure {
				if err == nil {
					t.Fatalf("parseRange(%q, %d) succeeded", test.value, test.size)
				}
				return
			}
			if err != nil || start != test.wantStart || end != test.wantEnd {
				t.Fatalf("parseRange(%q, %d) = %d, %d, %v", test.value, test.size, start, end, err)
			}
		})
	}
}

func TestMediaShortProbeAndExactRanges(t *testing.T) {
	directory := repositoryTempDir(t)
	data := bytes.Repeat([]byte{0x42}, 1024)
	if err := os.WriteFile(filepath.Join(directory, "short.m4a"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	server := &fixtureServer{directory: directory, faults: make(map[string]bool)}
	for _, test := range []struct {
		value      string
		wantRange  string
		wantStatus int
		wantBody   []byte
	}{
		{"bytes=0-65535", "bytes 0-1023/1024", http.StatusPartialContent, data},
		{"bytes=0-15", "bytes 0-15/1024", http.StatusPartialContent, data[:16]},
		{"bytes=1024-2047", "bytes */1024", http.StatusRequestedRangeNotSatisfiable, nil},
	} {
		t.Run(test.value, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/tracks/short-audio/audio.m4a", nil)
			request.Header.Set("X-Fixture-Lease", leaseHeader)
			request.Header.Set("Range", test.value)
			response := httptest.NewRecorder()
			server.media("short.m4a")(response, request)
			if response.Code != test.wantStatus || response.Header().Get("Content-Range") != test.wantRange || !bytes.Equal(response.Body.Bytes(), test.wantBody) {
				t.Fatalf("range response = %d %v (%d bytes)", response.Code, response.Header(), response.Body.Len())
			}
			if response.Code == http.StatusPartialContent && response.Header().Get("Content-Length") != strconv.Itoa(len(test.wantBody)) {
				t.Fatal("response length does not match EOF-clipped body")
			}
			record := server.requests[len(server.requests)-1]
			if record.Path != request.URL.Path || record.Range != test.value || record.Status != test.wantStatus || record.Bytes != len(test.wantBody) {
				t.Fatalf("request evidence = %+v", record)
			}
		})
	}
}

func TestMarkVideoSAPTypeOne(t *testing.T) {
	directory := repositoryTempDir(t)
	filename := filepath.Join(directory, "video.mp4")
	data := sidxFixture(0)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := markVideoSAPTypeOne(filename); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	sap := binary.BigEndian.Uint32(updated[len(updated)-4:])
	if sap>>31 != 1 || (sap>>28)&7 != 1 || sap&0x0fffffff != 123 {
		t.Fatalf("SAP flags = %#08x", sap)
	}
}

func TestMarkVideoSAPTypeOneRejectsUnprovenFixture(t *testing.T) {
	directory := repositoryTempDir(t)
	filename := filepath.Join(directory, "video.mp4")
	if err := os.WriteFile(filename, sidxFixture(2), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := markVideoSAPTypeOne(filename); err == nil {
		t.Fatal("non-zero SAP type was rewritten")
	}
}

func TestMarkVideoSAPTypeOneRejectsTruncatedSIDX(t *testing.T) {
	directory := repositoryTempDir(t)
	filename := filepath.Join(directory, "video.mp4")
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[:4], uint32(len(data)))
	copy(data[4:], "sidx")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := markVideoSAPTypeOne(filename); err == nil {
		t.Fatal("truncated SIDX was accepted")
	}
}

func repositoryTempDir(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(root, "fixture-test.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func sidxFixture(sapType uint32) []byte {
	data := make([]byte, 44)
	binary.BigEndian.PutUint32(data[0:4], uint32(len(data)))
	copy(data[4:8], "sidx")
	binary.BigEndian.PutUint32(data[12:16], 1)
	binary.BigEndian.PutUint32(data[16:20], 1000)
	binary.BigEndian.PutUint16(data[30:32], 1)
	binary.BigEndian.PutUint32(data[32:36], 1024)
	binary.BigEndian.PutUint32(data[36:40], 2000)
	binary.BigEndian.PutUint32(data[40:44], 1<<31|(sapType&7)<<28|123)
	return data
}
