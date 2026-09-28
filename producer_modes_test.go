package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ffmpegclient "github.com/killbus/rulego-ffmpeg-over-ip/client"
)

func TestProductionModesUseOnlySelectedInputsAndOneMember(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		t.Run(mode, func(t *testing.T) {
			manager := bareManager()
			defer manager.Close()
			manager.config.Root = t.TempDir()
			manager.inspectFn = manager.inspectSource
			var mu sync.Mutex
			var requests []string
			manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				mu.Lock()
				requests = append(requests, request.URL.Path+" "+request.Header.Get("Range"))
				mu.Unlock()
				return fixtureResponse(request, indexedFixture(request.URL.Path == "/video")), nil
			})}
			source := selectedLease(mode)
			inspection, err := manager.Inspect(context.Background(), inspectRequest{Source: source})
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			requests = nil
			mu.Unlock()
			segment := len(inspection.Segments) - 1
			member := fmt.Sprintf("%d.ts", segment)
			var runs int
			manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, output ffmpegclient.OutputFunc) (int, error) {
				runs++
				if output != nil || invocation.MaxOutputBytes != 67 || invocation.Program != "ffmpeg" {
					t.Fatalf("invocation=%+v callback=%v", invocation, output != nil)
				}
				args := append([]string(nil), invocation.Args...)
				inputs := 0
				for i, arg := range args {
					if arg != "-i" {
						continue
					}
					inputs++
					path := args[i+1]
					video := strings.Contains(filepath.Base(path), "-video-")
					want, label := indexedFixture(true)[:8], "VIDEO"
					media := "BBB"
					if !video {
						label, want, media = "AUDIO", indexedFixture(false)[:8], "CCC"
						if mode == "paired" {
							media = "BBBCCC"
						}
					}
					want = append(append([]byte(nil), want...), []byte(media)...)
					got, err := os.ReadFile(path)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("%s input=%q want=%q err=%v", label, got, want, err)
					}
					args[i+1] = label
				}
				entries, err := os.ReadDir(manager.config.Root)
				if err != nil || len(entries) != inputs {
					t.Fatalf("temporary inputs=%v err=%v", entries, err)
				}
				args[len(args)-1] = "OUTPUT"
				want := []string{"-y", "-hide_banner", "-loglevel", "error"}
				if mode == "paired" {
					want = append(want, "-i", "VIDEO", "-ss", "1.000000000", "-i", "AUDIO", "-map", "0:v:0", "-map", "1:a:0", "-copyts", "-c", "copy", "-shortest")
				} else if mode == "video" {
					want = append(want, "-i", "VIDEO", "-map", "0:v:0", "-copyts", "-c", "copy")
				} else {
					want = append(want, "-i", "AUDIO", "-map", "0:a:0", "-copyts", "-c", "copy")
				}
				want = append(want, "-mpegts_copyts", "1", "-mpegts_flags", "+initial_discontinuity", "-muxdelay", "0", "-f", "mpegts", "OUTPUT")
				if !reflect.DeepEqual(args, want) {
					t.Fatalf("args=%q want=%q", args, want)
				}
				return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], []byte("mpeg-ts"), 0o600)
			}
			request := productionRequest(source, inspection.Revision, manager.config.Root)
			request.Segment = segment
			result, err := manager.Produce(context.Background(), request)
			if err != nil || result.Member != member || result.Bytes != 7 || runs != 1 {
				t.Fatalf("result=%+v err=%v runs=%d", result, err, runs)
			}
			wantRanges := []string{"/video bytes=0-7", "/video bytes=67-69"}
			if mode == "audio" {
				wantRanges = []string{"/audio bytes=0-7", "/audio bytes=82-84"}
			} else if mode == "paired" {
				wantRanges = append(wantRanges, "/audio bytes=0-7", "/audio bytes=79-81", "/audio bytes=82-84")
			}
			mu.Lock()
			if !reflect.DeepEqual(requests, wantRanges) {
				t.Errorf("ranges=%v want=%v", requests, wantRanges)
			}
			before := len(requests)
			mu.Unlock()
			request.Segment++
			if _, err := manager.Produce(context.Background(), request); asProblem(err).kind != "invalid_input" {
				t.Fatalf("primary timeline bound not enforced: %v", err)
			}
			mu.Lock()
			if len(requests) != before || runs != 1 {
				t.Error("out-of-range segment performed production")
			}
			mu.Unlock()
			entries, err := os.ReadDir(manager.config.Root)
			if err != nil || len(entries) != 1 || entries[0].Name() != member {
				t.Fatalf("staged entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestProductionReinspectsRenewedSelectedLeases(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		for _, renewed := range selectedLease(mode).selectedTracks() {
			t.Run(mode+"/"+renewed.role, func(t *testing.T) {
				manager := bareManager()
				defer manager.Close()
				manager.config.Root = t.TempDir()
				manager.inspectFn = manager.inspectSource
				source := selectedLease(mode)
				var expected atomic.Pointer[mediaLease]
				expected.Store(&source)
				var probes atomic.Int32
				manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					video := request.URL.Path == "/video"
					want := expected.Load().Audio
					if video {
						want = expected.Load().Video
					}
					if want == nil || request.URL.String() != want.URL || request.Header.Get("Authorization") != want.Headers["Authorization"] {
						t.Errorf("unexpected access: %s authorization=%s", request.URL, request.Header.Get("Authorization"))
						return response(http.StatusForbidden, ""), nil
					}
					if request.Header.Get("Range") == "bytes=0-65535" {
						probes.Add(1)
					}
					return fixtureResponse(request, indexedFixture(video)), nil
				})}
				inspection, err := manager.Inspect(context.Background(), inspectRequest{Source: source})
				if err != nil {
					t.Fatal(err)
				}
				fresh := selectedLease(mode)
				track := fresh.Video
				if renewed.role == "audio" {
					track = fresh.Audio
				}
				track.URL += "?lease=renewed"
				track.Headers = map[string]string{"Authorization": "renewed"}
				expected.Store(&fresh)
				manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, _ ffmpegclient.OutputFunc) (int, error) {
					return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], []byte("mpeg-ts"), 0o600)
				}
				if _, err := manager.Produce(context.Background(), productionRequest(fresh, inspection.Revision, manager.config.Root)); err != nil {
					t.Fatal(err)
				}
				if got, want := probes.Load(), int32(2*len(fresh.selectedTracks())); got != want {
					t.Fatalf("discovery probes=%d want=%d", got, want)
				}
			})
		}
	}
}

func TestRevisionGateAllowsOnlyDiscoveryForModeOrEvidenceChanges(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		for _, change := range []string{"mode", "raw-index"} {
			t.Run(mode+"/"+change, func(t *testing.T) {
				manager := bareManager()
				defer manager.Close()
				manager.config.Root = t.TempDir()
				manager.inspectFn = manager.inspectSource
				var changed atomic.Bool
				var probes, production, runs atomic.Int32
				manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.Header.Get("Range") == "bytes=0-65535" {
						probes.Add(1)
					} else {
						production.Add(1)
					}
					data := indexedFixture(request.URL.Path == "/video")
					if changed.Load() && change == "raw-index" {
						binary.BigEndian.PutUint32(data[28:32], 100)
					}
					return fixtureResponse(request, data), nil
				})}
				manager.runFn = func(context.Context, ffmpegclient.Config, ffmpegclient.Invocation, ffmpegclient.OutputFunc) (int, error) {
					runs.Add(1)
					return 0, nil
				}
				source := selectedLease(mode)
				inspection, err := manager.Inspect(context.Background(), inspectRequest{Source: source})
				if err != nil {
					t.Fatal(err)
				}
				changed.Store(true)
				fresh := selectedLease(mode)
				if change == "mode" {
					if mode == "paired" {
						fresh = selectedLease("audio")
					} else {
						fresh = selectedLease("paired")
					}
				} else {
					for _, track := range []*mediaRepresentation{fresh.Video, fresh.Audio} {
						if track != nil {
							track.URL += "?new"
						}
					}
				}
				probes.Store(0)
				result, err := manager.Produce(context.Background(), productionRequest(fresh, inspection.Revision, manager.config.Root))
				if asProblem(err).kind != "revision_changed" || result.Member != "" || production.Load() != 0 || runs.Load() != 0 || probes.Load() != int32(len(fresh.selectedTracks())) {
					t.Fatalf("result=%+v err=%v probes=%d media=%d runs=%d", result, err, probes.Load(), production.Load(), runs.Load())
				}
			})
		}
	}
}

func TestSelectedProductionFailuresCleanInputsAndStayIsolated(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		for _, failure := range []string{"input-limit", "stale", "partial-input", "output-limit", "empty-output", "ffmpeg", "unsafe-path", "deadline", "canceled"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				manager := bareManager()
				defer manager.Close()
				manager.config.Root = t.TempDir()
				source := selectedLease(mode)
				revision := strings.Repeat("b", 64)
				manager.inspectFn = func(_ context.Context, lease mediaLease) (*sourceBundle, error) {
					return testBundle(lease, revision), nil
				}
				for _, other := range []string{"paired", "video", "audio"} {
					if _, err := manager.getBundle(context.Background(), selectedLease(other)); err != nil {
						t.Fatal(err)
					}
				}
				var reads, runs int
				manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					reads++
					if failure == "stale" || failure == "partial-input" && (mode == "paired" && reads == 3 || mode != "paired" && reads == 2) {
						return response(http.StatusForbidden, ""), nil
					}
					return fixtureResponse(request, []byte("initdataVID")), nil
				})}
				manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, _ ffmpegclient.OutputFunc) (int, error) {
					runs++
					if failure == "ffmpeg" {
						return 1, &ffmpegclient.Error{Kind: "exit", Message: "private backend details"}
					}
					size := 0
					if failure == "output-limit" {
						size = 68
					}
					return 0, os.WriteFile(invocation.Args[len(invocation.Args)-1], make([]byte, size), 0o600)
				}
				request := productionRequest(source, revision, manager.config.Root)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				want := "production_failed"
				switch failure {
				case "input-limit":
					request.MaxBytes, want = 10, "output_limit"
				case "stale", "partial-input":
					want = "source_stale"
				case "output-limit":
					want = "output_limit"
				case "unsafe-path":
					request.StagingDir, want = t.TempDir(), "unsafe_path"
				case "deadline":
					request.PublishBy, want = time.Now().Add(-time.Second), "timeout"
				case "canceled":
					cancel()
					want = "canceled"
				}
				result, err := manager.Produce(ctx, request)
				if asProblem(err).kind != want || result.Member != "" || strings.Contains(fmt.Sprint(err), "private") {
					t.Fatalf("result=%+v err=%v want=%s", result, err, want)
				}
				if (failure == "input-limit" || failure == "stale" || failure == "partial-input" || failure == "unsafe-path" || failure == "deadline" || failure == "canceled") && runs != 0 {
					t.Fatal("FFmpeg ran after input failure")
				}
				if (failure == "unsafe-path" || failure == "deadline" || failure == "canceled") && reads != 0 {
					t.Fatal("source read before safety gate")
				}
				entries, err := os.ReadDir(manager.config.Root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("failed production left inputs/output: %v err=%v", entries, err)
				}
				if want == "source_stale" {
					manager.mu.Lock()
					var remaining []string
					for _, other := range []string{"paired", "video", "audio"} {
						if manager.bundles[leaseKey(selectedLease(other))] != nil {
							remaining = append(remaining, other)
						}
					}
					manager.mu.Unlock()
					if len(remaining) != 2 || slices.Contains(remaining, mode) {
						t.Fatalf("stale invalidation affected other modes: %v", remaining)
					}
				}
			})
		}
	}
}

func TestSingleTrackRetriesProduceTheSameMember(t *testing.T) {
	for _, mode := range []string{"video", "audio"} {
		t.Run(mode, func(t *testing.T) {
			manager := bareManager()
			defer manager.Close()
			manager.config.Root = t.TempDir()
			manager.config.ProduceTimeoutMs = 5000
			source := selectedLease(mode)
			revision := strings.Repeat("a", 64)
			manager.inspectFn = func(_ context.Context, lease mediaLease) (*sourceBundle, error) {
				return testBundle(lease, revision), nil
			}
			var reads, runs int
			manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				reads++
				if request.URL.Path != "/"+mode {
					t.Errorf("absent track requested: %s", request.URL)
				}
				if reads < 3 {
					return response(http.StatusTooManyRequests, ""), nil
				}
				return fixtureResponse(request, []byte("initdataVID")), nil
			})}
			manager.runFn = func(_ context.Context, _ ffmpegclient.Config, invocation ffmpegclient.Invocation, _ ffmpegclient.OutputFunc) (int, error) {
				runs++
				path := invocation.Args[len(invocation.Args)-1]
				if path != filepath.Join(manager.config.Root, "0.ts") {
					t.Fatalf("retry selected a different member: %s", path)
				}
				if runs < 3 {
					if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
						t.Fatal(err)
					}
					return 0, &ffmpegclient.Error{Kind: "transport"}
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("partial output survived retry")
				}
				return 0, os.WriteFile(path, []byte("mpeg-ts"), 0o600)
			}
			request := productionRequest(source, revision, manager.config.Root)
			request.PublishBy = time.Now().Add(5 * time.Second)
			if result, err := manager.Produce(context.Background(), request); err != nil || result.Member != "0.ts" || reads != 4 || runs != 3 {
				t.Fatalf("result=%+v err=%v reads=%d runs=%d", result, err, reads, runs)
			}
			entries, err := os.ReadDir(manager.config.Root)
			if err != nil || len(entries) != 1 || entries[0].Name() != "0.ts" {
				t.Fatalf("retry entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestSingleTrackProductionCancellationCleansTemporaryInput(t *testing.T) {
	for _, mode := range []string{"video", "audio"} {
		for _, owner := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/owner=%t", mode, owner), func(t *testing.T) {
				manager := bareManager()
				defer manager.Close()
				manager.config.Root = t.TempDir()
				manager.config.ProduceTimeoutMs = 5000
				source := selectedLease(mode)
				revision := strings.Repeat("a", 64)
				manager.inspectFn = func(_ context.Context, lease mediaLease) (*sourceBundle, error) {
					return testBundle(lease, revision), nil
				}
				started := make(chan struct{}, 1)
				manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					started <- struct{}{}
					<-request.Context().Done()
					return nil, request.Context().Err()
				})}
				var runs atomic.Int32
				manager.runFn = func(context.Context, ffmpegclient.Config, ffmpegclient.Invocation, ffmpegclient.OutputFunc) (int, error) {
					runs.Add(1)
					return 0, nil
				}
				request := productionRequest(source, revision, manager.config.Root)
				request.PublishBy = time.Now().Add(5 * time.Second)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() {
					_, err := manager.Produce(ctx, request)
					done <- err
				}()
				awaitSignal(t, started)
				if owner {
					manager.Close()
				} else {
					cancel()
				}
				select {
				case err := <-done:
					if asProblem(err).kind != "canceled" || runs.Load() != 0 {
						t.Fatalf("err=%v runs=%d", err, runs.Load())
					}
				case <-time.After(2 * time.Second):
					t.Fatal("production did not cancel")
				}
				entries, err := os.ReadDir(manager.config.Root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("canceled inputs=%v err=%v", entries, err)
				}
			})
		}
	}
}
