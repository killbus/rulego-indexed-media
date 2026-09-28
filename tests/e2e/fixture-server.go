package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
)

const leaseHeader = "fixture-lease"

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,32}$`)
var byteRangePattern = regexp.MustCompile(`^bytes=([0-9]+)-([0-9]+)$`)

type requestRecord struct {
	Path   string `json:"path"`
	Range  string `json:"range,omitempty"`
	Status int    `json:"status"`
	Bytes  int    `json:"bytes"`
}

type fixtureServer struct {
	directory string

	mu       sync.Mutex
	requests []requestRecord
	faults   map[string]bool
}

func main() {
	listen := flag.String("listen", ":8080", "listen address")
	directory := flag.String("dir", ".", "fixture directory")
	normalizeVideoSAP := flag.Bool("normalize-video-sap", false, "mark generated video SIDX references as SAP type 1")
	flag.Parse()
	if *normalizeVideoSAP {
		if err := markVideoSAPTypeOne(filepath.Join(*directory, "video.mp4")); err != nil {
			log.Fatal(err)
		}
		return
	}

	server := &fixtureServer{directory: *directory, faults: make(map[string]bool)}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/run", server.resolve)
	mux.HandleFunc("/video.mp4", server.media("video.mp4"))
	mux.HandleFunc("/audio.m4a", server.media("audio.m4a"))
	// Test-only leases have isolated URLs/statistics and use nonzero timelines.
	for _, selection := range []string{"paired", "video", "audio"} {
		mux.HandleFunc("/tracks/"+selection+"/video.mp4", server.media("offset-video.mp4"))
		mux.HandleFunc("/tracks/"+selection+"/audio.m4a", server.media("offset-audio.m4a"))
	}
	mux.HandleFunc("/tracks/short-audio/audio.m4a", server.media("offset-short-audio.m4a"))
	mux.HandleFunc("/tracks/short-audio/video.mp4", server.media("offset-video.mp4"))
	mux.Handle("/playlists/", http.StripPrefix("/playlists/", http.FileServer(http.Dir(filepath.Join(*directory, "playlists")))))
	mux.HandleFunc("/stats", server.stats)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

// FFmpeg's fragmented MP4 muxer marks keyframe-aligned references as
// starts_with_SAP with an unspecified SAP type. The production contract
// deliberately requires explicit type 1, so make that fact explicit in the
// deterministic test fixture after generating it with fixed closed GOPs.
func markVideoSAPTypeOne(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	for offset := 0; offset+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		headerSize := 8
		if size == 1 {
			if offset+16 > len(data) {
				return errors.New("truncated extended MP4 box")
			}
			size64 := binary.BigEndian.Uint64(data[offset+8 : offset+16])
			if size64 > uint64(len(data)) {
				return errors.New("oversized MP4 box")
			}
			size = int(size64)
			headerSize = 16
		}
		if size < headerSize || offset+size > len(data) {
			return errors.New("invalid MP4 box")
		}
		if string(data[offset+4:offset+8]) != "sidx" {
			offset += size
			continue
		}
		cursor := offset + headerSize
		if cursor+12 > offset+size {
			return errors.New("truncated SIDX")
		}
		version := data[cursor]
		cursor += 4 + 4 + 4
		switch version {
		case 0:
			if cursor+8 > offset+size {
				return errors.New("truncated SIDX")
			}
			cursor += 8
		case 1:
			if cursor+16 > offset+size {
				return errors.New("truncated SIDX")
			}
			cursor += 16
		default:
			return errors.New("unsupported SIDX version")
		}
		if cursor+4 > offset+size {
			return errors.New("truncated SIDX")
		}
		count := int(binary.BigEndian.Uint16(data[cursor+2 : cursor+4]))
		cursor += 4
		if count == 0 || cursor+count*12 != offset+size {
			return errors.New("invalid SIDX references")
		}
		for index := 0; index < count; index++ {
			sapOffset := cursor + index*12 + 8
			sap := binary.BigEndian.Uint32(data[sapOffset : sapOffset+4])
			if sap>>31 != 1 || (sap>>28)&7 != 0 {
				return errors.New("fixture SIDX is not keyframe aligned with unspecified SAP type")
			}
			binary.BigEndian.PutUint32(data[sapOffset:sapOffset+4], sap|(1<<28))
		}
		return os.WriteFile(filename, data, 0o644)
	}
	return errors.New("SIDX box not found")
}

func (s *fixtureServer) resolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Args []string `json:"args"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	videoID := "fixture01"
	for _, argument := range request.Args {
		parsed, err := url.Parse(argument)
		if err == nil && parsed.Query().Get("v") != "" {
			videoID = parsed.Query().Get("v")
		}
	}
	if !videoIDPattern.MatchString(videoID) {
		http.Error(w, "invalid video id", http.StatusBadRequest)
		return
	}
	media := map[string]any{
		"id": videoID,
		"requested_formats": []map[string]any{
			{
				"url":          "http://ytdlp:8080/video.mp4",
				"http_headers": map[string]string{"X-Fixture-Lease": leaseHeader},
				"ext":          "mp4",
				"vcodec":       "avc1.42c00b",
				"acodec":       "none",
			},
			{
				"url":          "http://ytdlp:8080/audio.m4a",
				"http_headers": map[string]string{"X-Fixture-Lease": leaseHeader},
				"ext":          "m4a",
				"vcodec":       "none",
				"acodec":       "mp4a.40.2",
			},
		},
	}
	stdout, err := json.Marshal(media)
	if err != nil {
		http.Error(w, "fixture encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exit_code": 0,
		"stderr":    "",
		"stdout":    string(stdout) + "\n",
	})
}

func (s *fixtureServer) media(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("X-Fixture-Lease") != leaseHeader {
			s.record(requestRecord{Path: r.URL.Path, Range: r.Header.Get("Range"), Status: http.StatusForbidden})
			http.Error(w, "stale lease", http.StatusForbidden)
			return
		}
		file, err := os.Open(filepath.Join(s.directory, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			http.Error(w, "fixture unavailable", http.StatusInternalServerError)
			return
		}
		start, end, err := parseRange(r.Header.Get("Range"), info.Size())
		if err != nil {
			s.record(requestRecord{Path: r.URL.Path, Range: r.Header.Get("Range"), Status: http.StatusRequestedRangeNotSatisfiable})
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", info.Size()))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		injectFaults := r.URL.Path == "/"+name
		if injectFaults && r.Header.Get("Range") == "bytes=0-65535" {
			status := http.StatusTooManyRequests
			if name == "audio.m4a" {
				status = http.StatusServiceUnavailable
			}
			if s.firstFault(name + ":index") {
				s.record(requestRecord{Path: r.URL.Path, Range: r.Header.Get("Range"), Status: status})
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
				return
			}
		}
		if injectFaults && start > info.Size()/2 && s.firstFault(name+":distant") {
			if name == "audio.m4a" {
				s.record(requestRecord{Path: r.URL.Path, Range: r.Header.Get("Range"), Status: 0})
				panic(http.ErrAbortHandler)
			}
			length := end - start + 1
			partial := length / 2
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size()))
			w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.CopyN(w, io.NewSectionReader(file, start, partial), partial)
			s.record(requestRecord{Path: r.URL.Path, Range: r.Header.Get("Range"), Status: http.StatusPartialContent, Bytes: int(partial)})
			panic(http.ErrAbortHandler)
		}

		length := end - start + 1
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size()))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		written, copyErr := io.CopyN(w, io.NewSectionReader(file, start, length), length)
		if copyErr != nil {
			panic(http.ErrAbortHandler)
		}
		s.record(requestRecord{Path: r.URL.Path, Range: r.Header.Get("Range"), Status: http.StatusPartialContent, Bytes: int(written)})
	}
}

func (s *fixtureServer) stats(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	requests := append([]requestRecord(nil), s.requests...)
	faults := make([]string, 0, len(s.faults))
	for key := range s.faults {
		faults = append(faults, key)
	}
	s.mu.Unlock()
	sort.Strings(faults)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"requests": requests, "faults": faults})
}

func (s *fixtureServer) firstFault(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.faults[key] {
		return false
	}
	s.faults[key] = true
	return true
}

func (s *fixtureServer) record(record requestRecord) {
	s.mu.Lock()
	s.requests = append(s.requests, record)
	s.mu.Unlock()
	log.Printf("request path=%s range=%q status=%d bytes=%d", record.Path, record.Range, record.Status, record.Bytes)
}

func parseRange(value string, size int64) (int64, int64, error) {
	matches := byteRangePattern.FindStringSubmatch(value)
	if len(matches) != 3 {
		return 0, 0, errors.New("range is required")
	}
	start, startErr := strconv.ParseInt(matches[1], 10, 64)
	end, endErr := strconv.ParseInt(matches[2], 10, 64)
	if startErr != nil || endErr != nil || start < 0 || end < start || start >= size {
		return 0, 0, errors.New("invalid range")
	}
	// A satisfiable HTTP range clips at EOF. The plugin permits this only for
	// its initial probe; subsequent index and production reads remain exact.
	if end >= size {
		end = size - 1
	}
	return start, end, nil
}
