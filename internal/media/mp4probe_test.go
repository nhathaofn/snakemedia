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
