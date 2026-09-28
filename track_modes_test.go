package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ffmpegclient "github.com/killbus/rulego-ffmpeg-over-ip/client"
	"github.com/rulego/rulego"
	"github.com/rulego/rulego/api/types"
)

func selectedLease(mode string) mediaLease {
	source := testLease("https://media.invalid/video", "https://media.invalid/audio")
	if mode == "video" {
		source.Audio = nil
	} else if mode == "audio" {
		source.Video = nil
	}
	return source
}

func TestNodeRejectsMalformedSelectedTracksBeforeWork(t *testing.T) {
	node := &indexedMediaNode{}
	if err := node.Init(rulego.NewConfig(), types.Configuration{
		"root": t.TempDir(), "ffmpegAddress": "127.0.0.1:1", "ffmpegSecret": "test",
	}); err != nil {
		t.Fatal(err)
	}
	defer node.Destroy()
	manager, err := node.GetSafely()
	if err != nil {
		t.Fatal(err)
	}
	var work atomic.Int32
	manager.inspectFn = func(context.Context, mediaLease) (*sourceBundle, error) {
		work.Add(1)
		return nil, problem("unexpected", "discovery was reached")
	}
	manager.runFn = func(context.Context, ffmpegclient.Config, ffmpegclient.Invocation, ffmpegclient.OutputFunc) (int, error) {
		work.Add(1)
		return 0, nil
	}
	valid := selectedLease("paired")
	video, _ := json.Marshal(valid.Video)
	audio, _ := json.Marshal(valid.Audio)
	cases := map[string]string{
		"no-tracks":      `{"sourceKey":"asset"}`,
		"source-null":    `null`,
		"source-empty":   `{}`,
		"source-unknown": fmt.Sprintf(`{"sourceKey":"asset","video":%s,"unknown":1}`, video),
		"same-urls":      fmt.Sprintf(`{"sourceKey":"asset","video":%s,"audio":%s}`, video, strings.ReplaceAll(string(audio), "/audio", "/video")),
	}
	bad := map[string]string{
		"null": `null`, "empty": `{}`, "array": `[]`, "string": `"track"`, "number": `1`, "bool": `true`,
		"url-only":     `{"url":"https://media.invalid/partial"}`,
		"headers-only": `{"headers":{"Authorization":"lease"}}`,
		"codec-only":   `{"codec":"aac"}`,
	}
	for _, role := range []string{"video", "audio"} {
		track := video
		if role == "audio" {
			track = audio
		}
		cases[role+"-repeated/valid"] = fmt.Sprintf(`{"sourceKey":"asset",%q:%s,%q:%s}`, role, track, role, track)
		malformed := make(map[string]string)
		for name, raw := range bad {
			malformed[name] = raw
		}
		malformed["unknown"] = strings.TrimSuffix(string(track), "}") + `,"extra":true}`
		malformed["wrong-codec"] = strings.ReplaceAll(strings.ReplaceAll(string(track), "avc1.64002a", "vp9"), "mp4a.40.2", "opus")
		malformed["wrong-url"] = strings.ReplaceAll(string(track), "https://", "file://")
		malformed["bad-header"] = strings.ReplaceAll(string(track), "Authorization", `Bad\nHeader`)
		for name, raw := range malformed {
			v, a := string(video), string(audio)
			if role == "video" {
				v = raw
			} else {
				a = raw
			}
			cases[role+"/"+name] = fmt.Sprintf(`{"sourceKey":"asset","video":%s,"audio":%s}`, v, a)
			cases[role+"-alone/"+name] = fmt.Sprintf(`{"sourceKey":"asset",%q:%s}`, role, raw)
			cases[role+"-repeated/"+name] = fmt.Sprintf(`{"sourceKey":"asset",%q:%s,%q:%s}`, role, raw, role, track)
		}
	}
	for _, operation := range []string{"inspect", "produce"} {
		for name, source := range cases {
			t.Run(operation+"/"+name, func(t *testing.T) {
				fields := ""
				if operation == "produce" {
					fields = fmt.Sprintf(`,"expectedRevision":%q,"segment":0,"stagingDir":%q,"maxBytes":1024,"publishBy":%q`, strings.Repeat("a", 64), manager.config.Root, time.Now().Add(time.Hour).Format(time.RFC3339Nano))
				}
				ctx := &nodeResultContext{}
				node.OnMsg(ctx, types.NewMsgWithJsonData(fmt.Sprintf(`{"operation":%q,"source":%s%s}`, operation, source, fields)))
				var failure nodeError
				if err := json.Unmarshal(ctx.output.GetBytes(), &failure); err != nil {
					t.Fatal(err)
				}
				if ctx.relation != types.Failure || failure.Kind != "invalid_input" || work.Load() != 0 {
					t.Fatalf("relation=%s failure=%+v work=%d", ctx.relation, failure, work.Load())
				}
			})
		}
	}
	for _, suffix := range []string{` {}`, ` null`, ` true`} {
		if _, _, err := decodeRequest(fmt.Sprintf(`{"operation":"inspect","source":{"sourceKey":"asset","video":%s}}%s`, video, suffix)); err == nil {
			t.Fatalf("trailing JSON %q succeeded", suffix)
		}
	}
}

func TestSingleTrackRevisionFixedDigests(t *testing.T) {
	video := mediaIndex{InitSize: 0x0102030405060708, Raw: []byte{0, 0x76, 0x69, 0x64, 0x65, 0x6f, 0xff}}
	audio := mediaIndex{InitSize: 0x1112131415161718, Raw: []byte{0x80, 0x61, 0x75, 0x64, 0x69, 0x6f, 0, 0xfe}}
	for role, want := range map[string]string{
		"video": "4a2b69d8836874c9300b5f8cf1d4c7401f1768930a531878056a4f6e0113992b",
		"audio": "284385c83651ee8e63a104915dc1f9ee57afd0837f7893fae67e8e4dcca7b52d",
	} {
		source := mediaLease{SourceKey: "fixture:single-index-v1"}
		if role == "video" {
			source.Video = &mediaRepresentation{Container: "MP4", Codec: "AVC1.640028"}
		} else {
			source.Audio = &mediaRepresentation{Container: "M4A", Codec: "MP4A.40.2"}
		}
		if got := sourceRevision(source, video, audio); got != want {
			t.Fatalf("%s revision=%s want=%s", role, got, want)
		}
	}
}

// Synthetic indexed bytes exercise ranges and SIDX timing, not media playback.
func indexedFixture(video bool) []byte {
	var raw []byte
	if video {
		raw = makeSIDX(1000, 500, 0,
			testReference{size: 3, duration: 2000, startsAtSAP: true, sapType: 1},
			testReference{size: 3, duration: 3000, startsAtSAP: true, sapType: 1})
	} else {
		raw = makeSIDX(48000, 24000, 0,
			testReference{size: 3, duration: 48000}, testReference{size: 3, duration: 96000}, testReference{size: 3, duration: 48000})
	}
	data := []byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}
	data = append(data, raw...)
	return append(data, []byte("AAABBBCCC")...)
}

func fixtureResponse(request *http.Request, data []byte) *http.Response {
	var start, end int
	if _, err := fmt.Sscanf(request.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || start < 0 || start >= len(data) || end < start {
		return response(http.StatusRequestedRangeNotSatisfiable, "")
	}
	if end >= len(data) {
		end = len(data) - 1
	}
	return &http.Response{StatusCode: http.StatusPartialContent,
		Header: http.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", start, end, len(data))}},
		Body:   io.NopCloser(strings.NewReader(string(data[start : end+1])))}
}

func TestDiscoveryModesUsePrimaryTimelineAndSelectedRequests(t *testing.T) {
	for _, mode := range []string{"paired", "video", "audio"} {
		t.Run(mode, func(t *testing.T) {
			manager := bareManager()
			defer manager.Close()
			manager.inspectFn = manager.inspectSource
			var mu sync.Mutex
			var paths []string
			audioDone := make(chan struct{})
			manager.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				video := request.URL.Path == "/video"
				// Force audio delivery first to check role assignment.
				if mode == "paired" && video {
					select {
					case <-audioDone:
					case <-request.Context().Done():
						return nil, request.Context().Err()
					}
				}
				mu.Lock()
				paths = append(paths, request.URL.Path)
				mu.Unlock()
				if !video {
					defer close(audioDone)
				}
				return fixtureResponse(request, indexedFixture(video)), nil
			})}
			source := selectedLease(mode)
			result, err := manager.Inspect(context.Background(), inspectRequest{Source: source})
			if err != nil {
				t.Fatal(err)
			}
			wantDuration, wantSegments := 5.5, []segmentResult{{2}, {3}}
			if mode == "audio" {
				wantDuration, wantSegments = 4.5, []segmentResult{{1}, {2}, {1}}
			}
			if result.Duration != wantDuration || !reflect.DeepEqual(result.Segments, wantSegments) {
				t.Fatalf("inspection=%+v", result)
			}
			bundle, err := manager.getBundle(context.Background(), source)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "audio" && len(bundle.videoIdx.Refs) != 0 || mode == "video" && len(bundle.audioIdx.Refs) != 0 {
				t.Fatal("absent track acquired an index")
			}
			mu.Lock()
			defer mu.Unlock()
			wantPaths := []string{"/" + mode}
			if mode == "paired" {
				wantPaths = []string{"/audio", "/video"}
			}
			if !reflect.DeepEqual(paths, wantPaths) {
				t.Fatalf("requests=%v want=%v", paths, wantPaths)
			}
		})
	}
}

func TestSelectedRevisionEvidenceAndAccessIsolation(t *testing.T) {
	video, err := parseSIDX(indexedFixture(true)[8:64], 8, true)
	if err != nil {
		t.Fatal(err)
	}
	audio, err := parseSIDX(indexedFixture(false)[8:76], 8, false)
	if err != nil {
		t.Fatal(err)
	}
	revisions := make(map[string]bool)
	for _, mode := range []string{"paired", "video", "audio"} {
		source := selectedLease(mode)
		revision := sourceRevision(source, video, audio)
		if revisions[revision] {
			t.Fatal("selected modes share revision")
		}
		revisions[revision] = true
		for _, role := range source.selectedTracks() {
			for _, change := range []string{"url", "headers", "case", "codec", "container", "source-key", "init", "raw-time"} {
				changed := selectedLease(mode)
				representation, index := changed.Video, video
				if role.role == "audio" {
					representation, index = changed.Audio, audio
				}
				switch change {
				case "url":
					representation.URL += "?lease=new"
				case "headers":
					representation.Headers = map[string]string{"Authorization": "renewed"}
				case "case":
					representation.Container, representation.Codec = strings.ToUpper(representation.Container), strings.ToUpper(representation.Codec)
				case "codec":
					representation.Codec += "1"
				case "container":
					representation.Container = "other"
				case "source-key":
					changed.SourceKey += "new"
				case "init":
					index.InitSize++
				case "raw-time":
					index.Raw = append([]byte(nil), index.Raw...)
					binary.BigEndian.PutUint32(index.Raw[20:24], 100)
				}
				v, a := video, audio
				if role.role == "video" {
					v = index
				} else {
					a = index
				}
				stable := change == "url" || change == "headers" || change == "case"
				if (sourceRevision(changed, v, a) == revision) != stable {
					t.Fatalf("%s/%s/%s revision stability failed", mode, role.role, change)
				}
				if stable && leaseKey(changed) == leaseKey(source) {
					t.Fatalf("%s/%s/%s collapsed distinct leases", mode, role.role, change)
				}
			}
		}
		if mode == "video" && sourceRevision(source, video, mediaIndex{InitSize: 999, Raw: []byte("ignored")}) != revision ||
			mode == "audio" && sourceRevision(source, mediaIndex{InitSize: 999, Raw: []byte("ignored")}, audio) != revision {
			t.Fatal("absent track evidence changed revision")
		}
	}
}
