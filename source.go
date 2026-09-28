package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	ffmpegclient "github.com/killbus/rulego-ffmpeg-over-ip/client"
)

const maxCachedBundles = 256

type sourceBundle struct {
	source   mediaLease
	videoIdx mediaIndex
	audioIdx mediaIndex
	revision string
}

func (bundle *sourceBundle) primaryIndex() mediaIndex {
	if bundle.source.Video != nil {
		return bundle.videoIdx
	}
	return bundle.audioIdx
}

type inspectCall struct {
	done   chan struct{}
	bundle *sourceBundle
	err    error
}

type sourceManager struct {
	config nodeConfiguration
	client *http.Client
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	bundles  map[string]*sourceBundle
	inflight map[string]*inspectCall
	closed   bool

	inspectFn func(context.Context, mediaLease) (*sourceBundle, error)
	runFn     func(context.Context, ffmpegclient.Config, ffmpegclient.Invocation, ffmpegclient.OutputFunc) (int, error)
}

func newSourceManager(config nodeConfiguration) (*sourceManager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &sourceManager{
		config:   config,
		client:   &http.Client{},
		ctx:      ctx,
		cancel:   cancel,
		bundles:  make(map[string]*sourceBundle),
		inflight: make(map[string]*inspectCall),
		runFn:    ffmpegclient.Run,
	}
	m.inspectFn = m.inspectSource
	return m, nil
}

func (m *sourceManager) Close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
	}
	m.mu.Unlock()
}

func (m *sourceManager) Inspect(parent context.Context, request inspectRequest) (inspectResult, error) {
	ctx, stopOwner := m.operationContext(parent)
	defer stopOwner()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(m.config.IndexTimeoutMs)*time.Millisecond)
	defer cancel()
	bundle, err := m.getBundle(ctx, request.Source)
	if err != nil {
		return inspectResult{}, err
	}
	primary := bundle.primaryIndex()
	segments := make([]segmentResult, len(primary.Refs))
	for index, ref := range primary.Refs {
		segments[index] = segmentResult{Duration: float64(ref.Duration) / float64(primary.Timescale)}
	}
	return inspectResult{
		Revision: bundle.revision,
		Duration: float64(primary.durationTicks()) / float64(primary.Timescale),
		Segments: segments,
	}, nil
}

func (m *sourceManager) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(m.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (m *sourceManager) getBundle(ctx context.Context, source mediaLease) (*sourceBundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, contextProblem(err)
	}
	key := leaseKey(source)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, problem("canceled", "indexed media owner is shutting down")
	}
	if bundle := m.bundles[key]; bundle != nil {
		m.mu.Unlock()
		return bundle, nil
	}
	if call := m.inflight[key]; call != nil {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, contextProblem(ctx.Err())
		case <-call.done:
			return call.bundle, call.err
		}
	}
	call := &inspectCall{done: make(chan struct{})}
	m.inflight[key] = call
	m.mu.Unlock()

	bundle, err := m.inspectFn(ctx, source)
	m.mu.Lock()
	if err == nil && !m.closed {
		if len(m.bundles) >= maxCachedBundles {
			for candidate := range m.bundles {
				delete(m.bundles, candidate)
				break
			}
		}
		m.bundles[key] = bundle
	}
	call.bundle, call.err = bundle, err
	delete(m.inflight, key)
	close(call.done)
	m.mu.Unlock()
	return bundle, err
}

func leaseKey(source mediaLease) string {
	encoded, _ := json.Marshal(source)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func (m *sourceManager) invalidate(source mediaLease, bundle *sourceBundle) {
	key := leaseKey(source)
	m.mu.Lock()
	if m.bundles[key] == bundle {
		delete(m.bundles, key)
	}
	m.mu.Unlock()
}

func (m *sourceManager) inspectSource(ctx context.Context, source mediaLease) (*sourceBundle, error) {
	type indexResult struct {
		index mediaIndex
		err   error
		video bool
	}
	indexContext, cancel := context.WithCancel(ctx)
	defer cancel()
	tracks := source.selectedTracks()
	if len(tracks) == 0 {
		return nil, problem("invalid_input", "no media tracks selected")
	}
	results := make(chan indexResult, len(tracks))
	for _, track := range tracks {
		go func() {
			isVideo := track.role == "video"
			mediaIndex, err := m.discoverIndex(indexContext, track.representation, isVideo)
			results <- indexResult{index: mediaIndex, err: err, video: isVideo}
		}()
	}
	bundle := &sourceBundle{source: source}
	for range tracks {
		select {
		case <-indexContext.Done():
			return nil, contextProblem(indexContext.Err())
		case result := <-results:
			if result.err != nil {
				return nil, result.err
			}
			if result.video {
				bundle.videoIdx = result.index
			} else {
				bundle.audioIdx = result.index
			}
		}
	}
	if err := indexContext.Err(); err != nil {
		return nil, contextProblem(err)
	}
	bundle.revision = sourceRevision(source, bundle.videoIdx, bundle.audioIdx)
	return bundle, nil
}

func (m *sourceManager) doHTTP(ctx context.Context, makeRequest func() (*http.Request, error), maxBytes int64, responseLength func(*http.Response) (int64, bool)) ([]byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		request, err := makeRequest()
		if err != nil {
			return nil, problem("invalid_source", "source request is invalid")
		}
		response, requestErr := m.client.Do(request)
		expectedBytes, accepted := responseLength(response)
		if requestErr == nil && accepted && expectedBytes > 0 && expectedBytes <= maxBytes {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
			_ = response.Body.Close()
			if readErr == nil && int64(len(body)) == expectedBytes {
				return body, nil
			}
			if int64(len(body)) > maxBytes {
				return nil, problem("source_limit", "source response exceeded limit")
			}
		} else if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			switch response.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
				return nil, problem("source_stale", "source lease was rejected")
			}
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil, problem("invalid_source", "source range response is invalid")
			}
			if response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
				return nil, problem("source_unavailable", "source request was rejected")
			}
		}
		if ctx.Err() != nil {
			return nil, contextProblem(ctx.Err())
		}
		if attempt < 2 {
			if err := waitRetry(ctx, attempt); err != nil {
				return nil, err
			}
		}
	}
	return nil, problem("source_unavailable", "source request failed")
}

func waitRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(100*(1<<attempt)) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return contextProblem(ctx.Err())
	case <-timer.C:
		return nil
	}
}

func sourceRevision(source mediaLease, videoIndex, audioIndex mediaIndex) string {
	if source.Video == nil || source.Audio == nil {
		return singleTrackRevision(source, videoIndex, audioIndex)
	}
	type facts struct {
		SourceKey string              `json:"sourceKey"`
		Video     representationFacts `json:"video"`
		Audio     representationFacts `json:"audio"`
	}
	encoded, _ := json.Marshal(facts{
		SourceKey: source.SourceKey,
		Video:     stableFacts(*source.Video),
		Audio:     stableFacts(*source.Audio),
	})
	hash := sha256.New()
	_, _ = hash.Write(encoded)
	for _, index := range []mediaIndex{videoIndex, audioIndex} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], index.InitSize)
		_, _ = hash.Write(length[:])
		binary.BigEndian.PutUint64(length[:], uint64(len(index.Raw)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(index.Raw)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func singleTrackRevision(source mediaLease, videoIndex, audioIndex mediaIndex) string {
	track := source.selectedTracks()[0]
	index := videoIndex
	if track.role == "audio" {
		index = audioIndex
	}
	facts := struct {
		SourceKey      string              `json:"sourceKey"`
		Role           string              `json:"role"`
		Representation representationFacts `json:"representation"`
	}{source.SourceKey, track.role, stableFacts(track.representation)}
	encoded, _ := json.Marshal(facts)
	hash := sha256.New()
	_, _ = hash.Write([]byte("indexed-media-single-track-v1\x00"))
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(encoded)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write(encoded)
	binary.BigEndian.PutUint64(length[:], index.InitSize)
	_, _ = hash.Write(length[:])
	binary.BigEndian.PutUint64(length[:], uint64(len(index.Raw)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write(index.Raw)
	return hex.EncodeToString(hash.Sum(nil))
}

type representationFacts struct {
	Container string `json:"container"`
	Codec     string `json:"codec"`
}

func stableFacts(representation mediaRepresentation) representationFacts {
	return representationFacts{
		Container: strings.ToLower(representation.Container),
		Codec:     strings.ToLower(representation.Codec),
	}
}

func contextProblem(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return problem("timeout", "indexed media operation timed out")
	}
	return problem("canceled", "indexed media operation was canceled")
}
