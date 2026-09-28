package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const (
	initialIndexProbeBytes = 64 << 10
	maxIndexBytes          = 1 << 20
	maxTopLevelBoxes       = 1024
)

type mediaReference struct {
	Offset   uint64
	Size     uint64
	Start    uint64
	Duration uint64
}

type mediaIndex struct {
	InitSize  uint64
	Timescale uint64
	Refs      []mediaReference
	Raw       []byte
}

func (index mediaIndex) durationTicks() uint64 {
	if len(index.Refs) == 0 {
		return 0
	}
	last := index.Refs[len(index.Refs)-1]
	return last.Start + last.Duration
}

func (m *sourceManager) discoverIndex(ctx context.Context, format mediaRepresentation, requireSAP bool) (mediaIndex, error) {
	probe, err := m.fetchIndexProbe(ctx, format)
	if err != nil {
		return mediaIndex{}, err
	}
	var offset uint64
	for boxes := 0; boxes < maxTopLevelBoxes; boxes++ {
		header, err := m.boxHeader(ctx, format, probe, offset)
		if err != nil {
			return mediaIndex{}, err
		}
		size, headerSize, kind, err := decodeBoxHeader(header)
		if err != nil || size < headerSize || size == 0 || offset > math.MaxUint64-size {
			return mediaIndex{}, problem("invalid_source", "source MP4 index is malformed")
		}
		if kind == "sidx" {
			if size > maxIndexBytes {
				return mediaIndex{}, problem("source_limit", "source SIDX exceeded limit")
			}
			var raw []byte
			if offset <= uint64(len(probe)) && size <= uint64(len(probe))-offset {
				raw = append([]byte(nil), probe[offset:offset+size]...)
			} else {
				raw, err = m.fetchRange(ctx, format, offset, size)
				if err != nil {
					return mediaIndex{}, err
				}
				if uint64(len(raw)) != size {
					return mediaIndex{}, problem("invalid_source", "source SIDX is truncated")
				}
			}
			return parseSIDX(raw, offset, requireSAP)
		}
		offset += size
	}
	return mediaIndex{}, problem("source_limit", "source SIDX was not found")
}

func (m *sourceManager) boxHeader(ctx context.Context, format mediaRepresentation, probe []byte, offset uint64) ([]byte, error) {
	if offset <= uint64(len(probe)) && uint64(len(probe))-offset >= 16 {
		return probe[offset : offset+16], nil
	}
	header, err := m.fetchRange(ctx, format, offset, 16)
	if err != nil {
		return nil, err
	}
	if len(header) < 8 {
		return nil, problem("invalid_source", "source MP4 header is truncated")
	}
	return header, nil
}

func decodeBoxHeader(header []byte) (uint64, uint64, string, error) {
	if len(header) < 8 {
		return 0, 0, "", errors.New("truncated box")
	}
	size := uint64(binary.BigEndian.Uint32(header[:4]))
	headerSize := uint64(8)
	if size == 1 {
		if len(header) < 16 {
			return 0, 0, "", errors.New("truncated extended box")
		}
		size = binary.BigEndian.Uint64(header[8:16])
		headerSize = 16
	}
	return size, headerSize, string(header[4:8]), nil
}

func parseSIDX(raw []byte, absoluteOffset uint64, requireSAP bool) (mediaIndex, error) {
	size, headerSize, kind, err := decodeBoxHeader(raw)
	if err != nil || kind != "sidx" || size != uint64(len(raw)) || size < headerSize+24 {
		return mediaIndex{}, problem("invalid_source", "source SIDX is malformed")
	}
	cursor := headerSize
	version := raw[cursor]
	cursor += 4 // version and flags
	if cursor+8 > uint64(len(raw)) {
		return mediaIndex{}, problem("invalid_source", "source SIDX is truncated")
	}
	cursor += 4 // reference_ID
	timescale := uint64(binary.BigEndian.Uint32(raw[cursor : cursor+4]))
	cursor += 4
	if timescale == 0 {
		return mediaIndex{}, problem("invalid_source", "source SIDX timescale is invalid")
	}
	var earliest, firstOffset uint64
	switch version {
	case 0:
		if cursor+8 > uint64(len(raw)) {
			return mediaIndex{}, problem("invalid_source", "source SIDX is truncated")
		}
		earliest = uint64(binary.BigEndian.Uint32(raw[cursor : cursor+4]))
		firstOffset = uint64(binary.BigEndian.Uint32(raw[cursor+4 : cursor+8]))
		cursor += 8
	case 1:
		if cursor+16 > uint64(len(raw)) {
			return mediaIndex{}, problem("invalid_source", "source SIDX is truncated")
		}
		earliest = binary.BigEndian.Uint64(raw[cursor : cursor+8])
		firstOffset = binary.BigEndian.Uint64(raw[cursor+8 : cursor+16])
		cursor += 16
	default:
		return mediaIndex{}, problem("invalid_source", "source SIDX version is unsupported")
	}
	if cursor+4 > uint64(len(raw)) {
		return mediaIndex{}, problem("invalid_source", "source SIDX is truncated")
	}
	cursor += 2 // reserved
	count := uint64(binary.BigEndian.Uint16(raw[cursor : cursor+2]))
	cursor += 2
	if count == 0 || count > (uint64(len(raw))-cursor)/12 || cursor+count*12 != uint64(len(raw)) {
		return mediaIndex{}, problem("invalid_source", "source SIDX references are malformed")
	}
	if absoluteOffset > math.MaxUint64-size || absoluteOffset+size > math.MaxUint64-firstOffset {
		return mediaIndex{}, problem("invalid_source", "source SIDX offset overflow")
	}
	mediaOffset := absoluteOffset + size + firstOffset
	mediaTime := earliest
	refs := make([]mediaReference, 0, count)
	for index := uint64(0); index < count; index++ {
		reference := binary.BigEndian.Uint32(raw[cursor : cursor+4])
		duration := uint64(binary.BigEndian.Uint32(raw[cursor+4 : cursor+8]))
		sap := binary.BigEndian.Uint32(raw[cursor+8 : cursor+12])
		cursor += 12
		if reference>>31 != 0 || reference&0x7fffffff == 0 || duration == 0 {
			return mediaIndex{}, problem("invalid_source", "source SIDX contains an indirect or empty reference")
		}
		if requireSAP && (sap>>31 == 0 || (sap>>28)&7 != 1) {
			return mediaIndex{}, problem("invalid_source", "video SIDX reference is not SAP type 1")
		}
		referenceSize := uint64(reference & 0x7fffffff)
		if mediaOffset > math.MaxUint64-referenceSize || mediaTime > math.MaxUint64-duration {
			return mediaIndex{}, problem("invalid_source", "source SIDX reference overflow")
		}
		refs = append(refs, mediaReference{Offset: mediaOffset, Size: referenceSize, Start: mediaTime, Duration: duration})
		mediaOffset += referenceSize
		mediaTime += duration
	}
	return mediaIndex{
		InitSize: absoluteOffset, Timescale: timescale, Refs: refs,
		Raw: append([]byte(nil), raw...),
	}, nil
}

func (m *sourceManager) fetchRange(ctx context.Context, format mediaRepresentation, start, length uint64) ([]byte, error) {
	return m.fetchIndexRange(ctx, format, start, length, false)
}

func (m *sourceManager) fetchIndexProbe(ctx context.Context, format mediaRepresentation) ([]byte, error) {
	return m.fetchIndexRange(ctx, format, 0, initialIndexProbeBytes, true)
}

func (m *sourceManager) fetchIndexRange(ctx context.Context, format mediaRepresentation, start, length uint64, allowProbeEOF bool) ([]byte, error) {
	if length == 0 || length > maxIndexBytes || start > math.MaxUint64-(length-1) {
		return nil, problem("source_limit", "source range exceeded limit")
	}
	end := start + length - 1
	return m.doHTTP(ctx, func() (*http.Request, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, format.URL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		request.Header.Set("Accept-Encoding", "identity")
		for name, value := range format.Headers {
			if strings.EqualFold(name, "Range") || strings.EqualFold(name, "Accept-Encoding") || !validHeader(name, value) {
				continue
			}
			request.Header.Set(name, value)
		}
		return request, nil
	}, int64(length), func(response *http.Response) (int64, bool) {
		if validRangeResponse(response, start, end) {
			return int64(length), true
		}
		if allowProbeEOF && start == 0 && length == initialIndexProbeBytes {
			first, last, total, ok := responseRange(response)
			if ok && first == 0 && total > 0 && total < initialIndexProbeBytes && last == total-1 {
				return int64(total), true
			}
		}
		return 0, false
	})
}

var contentRangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+|\*)$`)

// A zero total represents the HTTP unknown-total form, never proven EOF.
func responseRange(response *http.Response) (start, end, total uint64, ok bool) {
	if response == nil || response.StatusCode != http.StatusPartialContent || response.Uncompressed {
		return 0, 0, 0, false
	}
	if len(response.Header.Values("Content-Range")) != 1 {
		return 0, 0, 0, false
	}
	for _, encoding := range response.Header.Values("Content-Encoding") {
		if encoding != "" && !strings.EqualFold(encoding, "identity") {
			return 0, 0, 0, false
		}
	}
	fields := contentRangePattern.FindStringSubmatch(response.Header.Get("Content-Range"))
	if fields == nil {
		return 0, 0, 0, false
	}
	start, startErr := strconv.ParseUint(fields[1], 10, 64)
	end, endErr := strconv.ParseUint(fields[2], 10, 64)
	if startErr != nil || endErr != nil || end < start {
		return 0, 0, 0, false
	}
	if fields[3] != "*" {
		var err error
		total, err = strconv.ParseUint(fields[3], 10, 64)
		if err != nil || total <= end {
			return 0, 0, 0, false
		}
	}
	return start, end, total, true
}

func validHeader(name, value string) bool {
	if name == "" || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character))) {
			return false
		}
	}
	return true
}
