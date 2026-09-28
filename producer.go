package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ffmpegclient "github.com/killbus/rulego-ffmpeg-over-ip/client"
)

func (m *sourceManager) Produce(parent context.Context, request produceRequest) (produceResult, error) {
	ctx, stopOwner := m.operationContext(parent)
	defer stopOwner()
	deadline := request.PublishBy
	configuredDeadline := time.Now().Add(time.Duration(m.config.ProduceTimeoutMs) * time.Millisecond)
	if configuredDeadline.Before(deadline) {
		deadline = configuredDeadline
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return produceResult{}, contextProblem(err)
	}
	directory, err := stagingDirectory(m.config.Root, request.StagingDir)
	if err != nil {
		return produceResult{}, problem("unsafe_path", "staging directory is outside the configured root")
	}
	bundle, err := m.getBundle(ctx, request.Source)
	if err != nil {
		return produceResult{}, err
	}
	if bundle.revision != request.ExpectedRevision {
		return produceResult{}, problem("revision_changed", "source revision changed")
	}
	result, err := m.produceOnce(ctx, bundle, request, directory)
	var sourceErr *sourceProblem
	if errors.As(err, &sourceErr) && sourceErr.kind == "source_stale" {
		m.invalidate(request.Source, bundle)
	}
	return result, err
}

func (m *sourceManager) produceOnce(ctx context.Context, bundle *sourceBundle, request produceRequest, directory string) (produceResult, error) {
	if request.Segment >= len(bundle.videoIdx.Refs) {
		return produceResult{}, problem("invalid_input", "segment is out of range")
	}
	videoRef := bundle.videoIdx.Refs[request.Segment]
	audioRefs, audioStart, err := overlappingAudio(bundle.audioIdx, videoRef, bundle.videoIdx.Timescale)
	if err != nil {
		return produceResult{}, err
	}
	videoFile, err := os.CreateTemp(directory, ".indexed-media-video-*.mp4")
	if err != nil {
		return produceResult{}, problem("storage", "temporary video could not be created")
	}
	videoPath := videoFile.Name()
	defer os.Remove(videoPath)
	audioFile, err := os.CreateTemp(directory, ".indexed-media-audio-*.m4a")
	if err != nil {
		_ = videoFile.Close()
		return produceResult{}, problem("storage", "temporary audio could not be created")
	}
	audioPath := audioFile.Name()
	defer os.Remove(audioPath)

	if err := m.writeInput(ctx, videoFile, bundle.source.Video, bundle.videoIdx, []mediaReference{videoRef}, request.MaxBytes); err != nil {
		_ = videoFile.Close()
		_ = audioFile.Close()
		return produceResult{}, err
	}
	if err := videoFile.Close(); err != nil {
		_ = audioFile.Close()
		return produceResult{}, problem("storage", "temporary video could not be closed")
	}
	if err := m.writeInput(ctx, audioFile, bundle.source.Audio, bundle.audioIdx, audioRefs, request.MaxBytes); err != nil {
		_ = audioFile.Close()
		return produceResult{}, err
	}
	if err := audioFile.Close(); err != nil {
		return produceResult{}, problem("storage", "temporary audio could not be closed")
	}

	member := strconv.Itoa(request.Segment) + ".ts"
	outputPath := filepath.Join(directory, member)
	_ = os.Remove(outputPath)
	trim := float64(videoRef.Start)/float64(bundle.videoIdx.Timescale) - float64(audioStart)/float64(bundle.audioIdx.Timescale)
	if trim < 0 && trim > -1e-9 {
		trim = 0
	}
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", videoPath,
		"-ss", formatSeconds(trim), "-i", audioPath,
		"-map", "0:v:0", "-map", "1:a:0",
		"-copyts", "-c", "copy", "-shortest",
		"-mpegts_copyts", "1", "-mpegts_flags", "+initial_discontinuity",
		"-muxdelay", "0", "-f", "mpegts", outputPath,
	}
	config := ffmpegclient.Config{
		Address: m.config.FFmpegAddress, AuthSecret: m.config.FFmpegSecret,
		DialTimeout: time.Duration(m.config.FFmpegDialTimeoutMs) * time.Millisecond,
	}
	invocation := ffmpegclient.Invocation{Program: "ffmpeg", Args: args, MaxOutputBytes: request.MaxBytes}
	for attempt := 0; attempt < 3; attempt++ {
		_ = os.Remove(outputPath)
		_, runErr := m.runFn(ctx, config, invocation, nil)
		if runErr == nil {
			break
		}
		safeErr := safeFFmpegError(runErr)
		if !isRetryable(safeErr) || attempt == 2 {
			return produceResult{}, safeErr
		}
		if err := waitRetry(ctx, attempt); err != nil {
			return produceResult{}, err
		}
	}
	info, err := os.Stat(outputPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		_ = os.Remove(outputPath)
		return produceResult{}, problem("production_failed", "segment was not produced")
	}
	if info.Size() > request.MaxBytes {
		_ = os.Remove(outputPath)
		return produceResult{}, problem("output_limit", "production byte limit exceeded")
	}
	return produceResult{Member: member, Bytes: info.Size()}, nil
}

func overlappingAudio(index mediaIndex, video mediaReference, videoTimescale uint64) ([]mediaReference, uint64, error) {
	videoEnd, overflow := addUint64(video.Start, video.Duration)
	if overflow {
		return nil, 0, problem("invalid_source", "video timeline overflow")
	}
	var selected []mediaReference
	var selectedStart uint64
	for _, reference := range index.Refs {
		audioEnd, audioOverflow := addUint64(reference.Start, reference.Duration)
		if audioOverflow {
			return nil, 0, problem("invalid_source", "audio timeline overflow")
		}
		if fractionLess(video.Start, videoTimescale, audioEnd, index.Timescale) && fractionLess(reference.Start, index.Timescale, videoEnd, videoTimescale) {
			if len(selected) == 0 {
				selectedStart = reference.Start
			}
			selected = append(selected, reference)
		}
	}
	if len(selected) == 0 {
		return nil, 0, problem("invalid_source", "audio does not cover video segment")
	}
	return selected, selectedStart, nil
}

func fractionLess(left, leftScale, right, rightScale uint64) bool {
	leftHigh, leftLow := bits.Mul64(left, rightScale)
	rightHigh, rightLow := bits.Mul64(right, leftScale)
	return leftHigh < rightHigh || leftHigh == rightHigh && leftLow < rightLow
}

func addUint64(left, right uint64) (uint64, bool) {
	if right > math.MaxUint64-left {
		return 0, true
	}
	return left + right, false
}

func (m *sourceManager) writeInput(ctx context.Context, output *os.File, format mediaRepresentation, index mediaIndex, references []mediaReference, maxBytes int64) error {
	var written int64
	copyPart := func(start, length uint64) error {
		if length == 0 || length > uint64(maxBytes-written) {
			return problem("output_limit", "source input exceeds byte limit")
		}
		count, err := m.copySourceRange(ctx, output, format, start, length)
		written += count
		return err
	}
	if index.InitSize == 0 {
		return problem("invalid_source", "source initialization section is missing")
	}
	if err := copyPart(0, index.InitSize); err != nil {
		return err
	}
	for _, reference := range references {
		if err := copyPart(reference.Offset, reference.Size); err != nil {
			return err
		}
	}
	if err := output.Sync(); err != nil {
		return problem("storage", "temporary input could not be synchronized")
	}
	return nil
}

func (m *sourceManager) copySourceRange(ctx context.Context, output *os.File, format mediaRepresentation, start, length uint64) (int64, error) {
	if length == 0 || start > math.MaxUint64-(length-1) || length > math.MaxInt64 {
		return 0, problem("source_limit", "source range exceeded limit")
	}
	position, err := output.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, problem("storage", "temporary input seek failed")
	}
	end := start + length - 1
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := output.Seek(position, io.SeekStart); err != nil {
			return 0, problem("storage", "temporary input seek failed")
		}
		if err := output.Truncate(position); err != nil {
			return 0, problem("storage", "temporary input reset failed")
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, format.URL, nil)
		if err != nil {
			return 0, problem("invalid_source", "source request is invalid")
		}
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		request.Header.Set("Accept-Encoding", "identity")
		for name, value := range format.Headers {
			if strings.EqualFold(name, "Range") || strings.EqualFold(name, "Accept-Encoding") || !validHeader(name, value) {
				continue
			}
			request.Header.Set(name, value)
		}
		response, requestErr := m.client.Do(request)
		if requestErr == nil && validRangeResponse(response, start, end) {
			count, copyErr := io.CopyN(output, response.Body, int64(length))
			var extra [1]byte
			extraCount, extraErr := response.Body.Read(extra[:])
			_ = response.Body.Close()
			if copyErr == nil && extraCount == 0 && extraErr == io.EOF {
				return count, nil
			}
		} else if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
				return 0, problem("source_stale", "source lease was rejected")
			}
			if response.StatusCode != http.StatusTooManyRequests && response.StatusCode < 500 {
				return 0, problem("source_unavailable", "source range was rejected")
			}
		}
		if ctx.Err() != nil {
			return 0, contextProblem(ctx.Err())
		}
		if attempt < 2 {
			if err := waitRetry(ctx, attempt); err != nil {
				return 0, err
			}
		}
	}
	return 0, problem("source_unavailable", "source range failed")
}

func validRangeResponse(response *http.Response, start, end uint64) bool {
	if response == nil || response.StatusCode != http.StatusPartialContent {
		return false
	}
	prefix := fmt.Sprintf("bytes %d-%d/", start, end)
	return strings.HasPrefix(response.Header.Get("Content-Range"), prefix)
}

func isRetryable(err error) bool {
	var retryable *retryableProblem
	return errors.As(err, &retryable)
}

type retryableProblem struct{ err *sourceProblem }

func (e *retryableProblem) Error() string { return e.err.Error() }
func (e *retryableProblem) Unwrap() error { return e.err }

func safeFFmpegError(err error) error {
	var remote *ffmpegclient.Error
	if !errors.As(err, &remote) {
		var network net.Error
		if errors.As(err, &network) {
			return &retryableProblem{err: problem("production_failed", "remote media production failed")}
		}
		return problem("production_failed", "remote media production failed")
	}
	switch remote.Kind {
	case "output_limit":
		return problem("output_limit", "production byte limit exceeded")
	case "canceled":
		return problem("canceled", "production was canceled")
	case "transport", "server", "timeout":
		return &retryableProblem{err: problem("production_failed", "remote media production failed")}
	default:
		return problem("production_failed", "remote media production failed")
	}
}

func stagingDirectory(root, requested string) (string, error) {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootPath, err = filepath.EvalSymlinks(rootPath)
	if err != nil {
		return "", err
	}
	requestedPath, err := filepath.Abs(requested)
	if err != nil {
		return "", err
	}
	requestedPath, err = filepath.EvalSymlinks(requestedPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(requestedPath)
	if err != nil || !info.IsDir() {
		return "", errors.New("staging path is not a directory")
	}
	relative, err := filepath.Rel(rootPath, requestedPath)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("staging path escapes root")
	}
	return requestedPath, nil
}
