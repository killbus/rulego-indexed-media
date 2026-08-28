package main

import (
	"encoding/binary"
	"math"
	"testing"
)

type testReference struct {
	size, duration        uint32
	indirect, startsAtSAP bool
	sapType               uint8
}

func makeSIDX(timescale uint32, earliest, firstOffset uint64, references ...testReference) []byte {
	version := byte(0)
	bodySize := 4 + 4 + 4 + 8 + 4 + len(references)*12
	if earliest > math.MaxUint32 || firstOffset > math.MaxUint32 {
		version, bodySize = 1, bodySize+8
	}
	raw := make([]byte, 8+bodySize)
	binary.BigEndian.PutUint32(raw, uint32(len(raw)))
	copy(raw[4:8], "sidx")
	position := 8
	raw[position] = version
	position += 4
	binary.BigEndian.PutUint32(raw[position:], 1)
	position += 4
	binary.BigEndian.PutUint32(raw[position:], timescale)
	position += 4
	if version == 0 {
		binary.BigEndian.PutUint32(raw[position:], uint32(earliest))
		binary.BigEndian.PutUint32(raw[position+4:], uint32(firstOffset))
		position += 8
	} else {
		binary.BigEndian.PutUint64(raw[position:], earliest)
		binary.BigEndian.PutUint64(raw[position+8:], firstOffset)
		position += 16
	}
	binary.BigEndian.PutUint16(raw[position+2:], uint16(len(references)))
	position += 4
	for _, reference := range references {
		word := reference.size
		if reference.indirect {
			word |= 1 << 31
		}
		binary.BigEndian.PutUint32(raw[position:], word)
		binary.BigEndian.PutUint32(raw[position+4:], reference.duration)
		sap := uint32(reference.sapType) << 28
		if reference.startsAtSAP {
			sap |= 1 << 31
		}
		binary.BigEndian.PutUint32(raw[position+8:], sap)
		position += 12
	}
	return raw
}

func TestParseSIDXDirectSAPAndOffsets(t *testing.T) {
	raw := makeSIDX(1000, 5, 7,
		testReference{size: 100, duration: 2000, startsAtSAP: true, sapType: 1},
		testReference{size: 120, duration: 3000, startsAtSAP: true, sapType: 1})
	index, err := parseSIDX(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	if index.Refs[0].Offset != uint64(40+len(raw)+7) || index.Refs[1].Offset != index.Refs[0].Offset+100 {
		t.Fatalf("offsets = %+v", index.Refs)
	}
	if index.durationTicks() != 5005 || index.Timescale != 1000 {
		t.Fatalf("index = %+v", index)
	}
}

func TestParseSIDXRejectsMalformedReferences(t *testing.T) {
	tests := [][]byte{
		makeSIDX(1000, 0, 0, testReference{size: 10, duration: 10, indirect: true}),
		makeSIDX(1000, 0, 0, testReference{size: 10, duration: 10, startsAtSAP: true, sapType: 2}),
		makeSIDX(0, 0, 0, testReference{size: 10, duration: 10, startsAtSAP: true, sapType: 1}),
		makeSIDX(1000, 0, 0, testReference{size: 0, duration: 10, startsAtSAP: true, sapType: 1}),
	}
	for caseIndex, raw := range tests {
		if _, err := parseSIDX(raw, 0, true); err == nil {
			t.Fatalf("case %d succeeded", caseIndex)
		}
	}
	raw := makeSIDX(1000, 0, 0, testReference{size: 10, duration: 10, startsAtSAP: true, sapType: 1})
	if _, err := parseSIDX(raw[:len(raw)-1], 0, true); err == nil {
		t.Fatal("truncated SIDX succeeded")
	}
}

func TestOverlappingAudioUsesMinimumReferences(t *testing.T) {
	audio := mediaIndex{Timescale: 10, Refs: []mediaReference{
		{Start: 0, Duration: 10}, {Start: 10, Duration: 10}, {Start: 20, Duration: 10}, {Start: 30, Duration: 10},
	}}
	selected, start, err := overlappingAudio(audio, mediaReference{Start: 15, Duration: 10}, 10)
	if err != nil || len(selected) != 2 || start != 10 || selected[0].Start != 10 || selected[1].Start != 20 {
		t.Fatalf("selected=%+v start=%d err=%v", selected, start, err)
	}
}

func TestFractionLessHandlesOverflow(t *testing.T) {
	if !fractionLess(math.MaxUint64-1, math.MaxUint64, math.MaxUint64, math.MaxUint64) {
		t.Fatal("large fraction comparison failed")
	}
}
