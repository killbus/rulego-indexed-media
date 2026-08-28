package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ffmpegclient "github.com/killbus/rulego-ffmpeg-over-ip/client"
)

func rangeServer(t *testing.T, failures *atomic.Int32) *httptest.Server {
	t.Helper()
	data := []byte("initdataVID")
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		wantAuthorization := "video-lease"
		if request.URL.Path == "/audio" {
			wantAuthorization = "audio-lease"
		}
		if got := request.Header.Get("Authorization"); got != wantAuthorization {
			t.Errorf("%s authorization = %q, want %q", request.URL.Path, got, wantAuthorization)
		}
		if failures != nil && failures.Add(-1) >= 0 {
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(request.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || start < 0 || end < start || end >= len(data) {
			writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(data[start : end+1])
	}))
}

func productionManager(t *testing.T, serverURL, revision string) (*sourceManager, string, mediaLease) {
	t.Helper()
	root := t.TempDir()
	staging := filepath.Join(root, "resource")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	source := testLease(serverURL+"/video", serverURL+"/audio")
	bundle := testBundle(source, revision)
	manager := bareManager()
	manager.config.Root = root
	manager.config.FFmpegDialTimeoutMs = 1000
	manager.client = http.DefaultClient
	manager.inspectFn = func(context.Context, mediaLease) (*sourceBundle, error) { return bundle, nil }
	return manager, staging, source
}

func productionRequest(source mediaLease, revision, staging string) produceRequest {
	return produceRequest{
		Source: source, ExpectedRevision: revision, Segment: 0,
		StagingDir: staging, MaxBytes: 67, PublishBy: time.Now().Add(time.Second),
	}
}

func TestProduceUsesBoundedRemoteFileOutputAndCleansInputs(t *testing.T) {
	var failures atomic.Int32
	failures.Store(2)
	server := rangeServer(t, &failures)
	defer server.Close()
	revision := strings.Repeat("b", 64)
	manager, staging, source := productionManager(t, server.URL, revision)
	defer manager.Close()
	manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, output ffmpegclient.OutputFunc) (int, error) {
		if output != nil {
			t.Fatal("stdout callback must be nil")
		}
		if invocation.MaxOutputBytes != 67 {
			t.Fatalf("MaxOutputBytes = %d", invocation.MaxOutputBytes)
		}
		return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], []byte("mpeg-ts"), 0o600)
	}
	result, err := manager.Produce(context.Background(), productionRequest(source, revision, staging))
	if err != nil {
		t.Fatal(err)
	}
	if result.Member != "0.ts" || result.Bytes != int64(len("mpeg-ts")) {
		t.Fatalf("result = %+v", result)
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "0.ts" {
		t.Fatalf("staged entries = %v", entries)
	}
}

func TestProduceUsesRefreshedLeaseURLAndHeaders(t *testing.T) {
	server := rangeServer(t, nil)
	defer server.Close()
	revision := strings.Repeat("b", 64)
	manager, staging, stale := productionManager(t, server.URL+"/stale", revision)
	defer manager.Close()
	stale.Video.Headers["Authorization"] = "stale-lease"
	stale.Audio.Headers["Authorization"] = "stale-lease"
	var inspections atomic.Int32
	manager.inspectFn = func(_ context.Context, source mediaLease) (*sourceBundle, error) {
		inspections.Add(1)
		return testBundle(source, revision), nil
	}
	if _, err := manager.getBundle(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	fresh := testLease(server.URL+"/video", server.URL+"/audio")
	manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, _ ffmpegclient.OutputFunc) (int, error) {
		return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], []byte("mpeg-ts"), 0o600)
	}
	if _, err := manager.Produce(context.Background(), productionRequest(fresh, revision, staging)); err != nil {
		t.Fatal(err)
	}
	if inspections.Load() != 2 {
		t.Fatalf("index inspections = %d, want 2", inspections.Load())
	}
}

func TestProduceRejectsUnsafePathBeforeInspection(t *testing.T) {
	server := rangeServer(t, nil)
	defer server.Close()
	revision := strings.Repeat("c", 64)
	manager, _, source := productionManager(t, server.URL, revision)
	defer manager.Close()
	var inspections atomic.Int32
	manager.inspectFn = func(context.Context, mediaLease) (*sourceBundle, error) {
		inspections.Add(1)
		return nil, nil
	}
	request := productionRequest(source, revision, t.TempDir())
	_, err := manager.Produce(context.Background(), request)
	if failure := asProblem(err); failure.kind != "unsafe_path" || inspections.Load() != 0 {
		t.Fatalf("inspections=%d error=%#v", inspections.Load(), err)
	}
}

func TestProduceRejectsRevisionDriftBeforeSourceRead(t *testing.T) {
	server := rangeServer(t, nil)
	defer server.Close()
	expected := strings.Repeat("d", 64)
	manager, staging, source := productionManager(t, server.URL, strings.Repeat("e", 64))
	defer manager.Close()
	var runs atomic.Int32
	manager.runFn = func(context.Context, ffmpegclient.Config, ffmpegclient.Invocation, ffmpegclient.OutputFunc) (int, error) {
		runs.Add(1)
		return 0, nil
	}
	_, err := manager.Produce(context.Background(), productionRequest(source, expected, staging))
	if failure := asProblem(err); failure.kind != "revision_changed" || runs.Load() != 0 {
		t.Fatalf("runs=%d error=%#v", runs.Load(), err)
	}
}

func TestStaleProductionInvalidatesCachedLease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	revision := strings.Repeat("e", 64)
	manager, staging, source := productionManager(t, server.URL, revision)
	defer manager.Close()
	bundle, err := manager.getBundle(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Produce(context.Background(), productionRequest(source, revision, staging))
	if failure := asProblem(err); failure.kind != "source_stale" {
		t.Fatalf("error = %#v", err)
	}
	manager.mu.Lock()
	cached := manager.bundles[leaseKey(source)]
	manager.mu.Unlock()
	if cached == bundle {
		t.Fatal("stale lease remained cached")
	}
}

func TestFFmpegRetriesTransportButNotExit(t *testing.T) {
	server := rangeServer(t, nil)
	defer server.Close()
	revision := strings.Repeat("f", 64)
	for _, kind := range []string{"transport", "server", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			manager, staging, source := productionManager(t, server.URL, revision)
			defer manager.Close()
			var runs atomic.Int32
			manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, _ ffmpegclient.OutputFunc) (int, error) {
				if runs.Add(1) < 3 {
					return 0, &ffmpegclient.Error{Kind: kind, Message: "temporary"}
				}
				return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], []byte("mpeg-ts"), 0o600)
			}
			if _, err := manager.Produce(context.Background(), productionRequest(source, revision, staging)); err != nil || runs.Load() != 3 {
				t.Fatalf("runs=%d err=%v", runs.Load(), err)
			}
		})
	}
	t.Run("exit", func(t *testing.T) {
		manager, staging, source := productionManager(t, server.URL, revision)
		defer manager.Close()
		var runs atomic.Int32
		manager.runFn = func(context.Context, ffmpegclient.Config, ffmpegclient.Invocation, ffmpegclient.OutputFunc) (int, error) {
			runs.Add(1)
			return 1, &ffmpegclient.Error{Kind: "exit", Message: "deterministic"}
		}
		_, err := manager.Produce(context.Background(), productionRequest(source, revision, staging))
		if failure := asProblem(err); failure.kind != "production_failed" || runs.Load() != 1 {
			t.Fatalf("runs=%d error=%#v", runs.Load(), err)
		}
	})
}

func TestProduceDeletesOutputThatExceedsLimit(t *testing.T) {
	server := rangeServer(t, nil)
	defer server.Close()
	revision := strings.Repeat("a", 64)
	manager, staging, source := productionManager(t, server.URL, revision)
	defer manager.Close()
	manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, _ ffmpegclient.OutputFunc) (int, error) {
		return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], make([]byte, 68), 0o600)
	}
	_, err := manager.Produce(context.Background(), productionRequest(source, revision, staging))
	if failure := asProblem(err); failure.kind != "output_limit" {
		t.Fatalf("error = %#v", failure)
	}
	if _, err := os.Stat(filepath.Join(staging, "0.ts")); !os.IsNotExist(err) {
		t.Fatalf("oversized output remains: %v", err)
	}
}

func TestProduceHonorsExpiredPublishDeadline(t *testing.T) {
	manager := bareManager()
	defer manager.Close()
	manager.config.Root = t.TempDir()
	var inspections atomic.Int32
	manager.inspectFn = func(context.Context, mediaLease) (*sourceBundle, error) {
		inspections.Add(1)
		return nil, nil
	}
	request := productionRequest(testLease("https://media.invalid/v", "https://media.invalid/a"), strings.Repeat("a", 64), manager.config.Root)
	request.PublishBy = time.Now().Add(-time.Second)
	_, err := manager.Produce(context.Background(), request)
	if failure := asProblem(err); failure.kind != "timeout" || inspections.Load() != 0 {
		t.Fatalf("inspections=%d error=%#v", inspections.Load(), err)
	}
}

func TestErrorsDoNotExposeBackendDetails(t *testing.T) {
	detail := "backend-internal-detail"
	err := safeFFmpegError(&ffmpegclient.Error{Kind: "server", Message: detail})
	if strings.Contains(err.Error(), detail) {
		t.Fatalf("remote payload leaked: %v", err)
	}
	if message := asProblem(fmt.Errorf("GET https://example.invalid/?lease=%s: disconnected", detail)).Error(); strings.Contains(message, detail) {
		t.Fatalf("source URL leaked: %s", message)
	}
}
