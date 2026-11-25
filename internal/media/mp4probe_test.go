package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func box(kind string, body ...[]byte) []byte {
	size := 8
	for _, b := range body {
		size += len(b)
	}
	out := make([]byte, 8, size)
	binary.BigEndian.PutUint32(out, uint32(size))
	copy(out[4:], kind)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

// testMP4 builds the smallest moov a player needs to report size and length.
func testMP4(width, height uint32, rotated bool) []byte {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 1000) // timescale
	binary.BigEndian.PutUint32(mvhd[16:], 4500) // 4.5 s
	tkhd := make([]byte, 84)
	matrix := tkhd[40:76]
	if rotated {
		binary.BigEndian.PutUint32(matrix[4:], 0x00010000)
		binary.BigEndian.PutUint32(matrix[12:], 0xFFFF0000)
	} else {
		binary.BigEndian.PutUint32(matrix[0:], 0x00010000)
		binary.BigEndian.PutUint32(matrix[16:], 0x00010000)
	}
	binary.BigEndian.PutUint32(tkhd[76:], width<<16)
	binary.BigEndian.PutUint32(tkhd[80:], height<<16)
	hdlr := make([]byte, 24)
	copy(hdlr[8:], "vide")
	trak := box("trak", box("tkhd", tkhd), box("mdia", box("hdlr", hdlr)))
	return append(box("ftyp", []byte("isom0000")), box("moov", box("mvhd", mvhd), trak)...)
}

func TestProbeMP4ReadsSizeDurationAndRotation(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		rotated       bool
		width, height int
	}{"landscape.mp4": {false, 640, 360}, "portrait.mov": {true, 360, 640}} {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, testMP4(640, 360, tc.rotated), 0o600); err != nil {
			t.Fatal(err)
		}
		meta, err := probeMP4(file)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if meta.Width != tc.width || meta.Height != tc.height || meta.DurationSeconds != 4.5 {
			t.Fatalf("%s: got %+v", name, meta)
		}
	}
}

func TestProbeMP4RejectsFilesWithoutVideoHeaders(t *testing.T) {
	file := filepath.Join(t.TempDir(), "empty.mp4")
	if err := os.WriteFile(file, box("ftyp", []byte("isom0000")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := probeMP4(file); err == nil {
		t.Fatal("a file without moov was accepted")
	}
}

func TestProbeMP4HandlesSizeOneOverflowAndEOFMoov(t *testing.T) {
	dir := t.TempDir()
	// I-4a: 64-bit extended size exceeding math.MaxInt64 must error instead of seeking backwards in a loop.
	overflow := make([]byte, 16)
	binary.BigEndian.PutUint32(overflow[0:4], 1)
	copy(overflow[4:8], "free")
	binary.BigEndian.PutUint64(overflow[8:16], ^uint64(0)-10)
	overflowFile := filepath.Join(dir, "overflow.mp4")
	if err := os.WriteFile(overflowFile, overflow, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := probeMP4(overflowFile); err == nil {
		t.Fatal("expected error for uint64 box size overflowing int64")
	}

	// I-4b: Trailing top-level moov box with size=0 ("extends to EOF") is valid in ISO BMFF.
	valid := testMP4(640, 360, false)
	// testMP4 produces ftyp + moov; rewrite moov's 32-bit size to 0 (to EOF).
	ftypLen := int(binary.BigEndian.Uint32(valid[0:4]))
	binary.BigEndian.PutUint32(valid[ftypLen:ftypLen+4], 0)
	eofMoovFile := filepath.Join(dir, "eof-moov.mp4")
	if err := os.WriteFile(eofMoovFile, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := probeMP4(eofMoovFile)
	if err != nil {
		t.Fatalf("size=0 trailing moov failed: %v", err)
	}
	if meta.Width != 640 || meta.Height != 360 || meta.DurationSeconds != 4.5 {
		t.Fatalf("got %+v", meta)
	}
}
