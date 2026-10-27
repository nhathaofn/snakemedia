package media

import (
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"  // DecodeConfig for GIF
	_ "image/jpeg" // DecodeConfig for JPEG
	_ "image/png"  // DecodeConfig for PNG
	"io"
	"os"
)

// ImageSize reads an image's width and height from its header: WebP (which
// the standard library cannot decode), PNG, JPEG or GIF.
func ImageSize(file string) (int, int, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	head := make([]byte, 30)
	n, _ := io.ReadFull(f, head)
	if w, h, ok := webpSize(head[:n]); ok {
		return w, h, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, 0, err
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, errors.New("Không đọc được kích thước ảnh.")
	}
	return cfg.Width, cfg.Height, nil
}

// webpSize reads a WebP header: the canvas of VP8X, or the frame of a lossy
// (VP8) or lossless (VP8L) image.
func webpSize(b []byte) (int, int, bool) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	switch string(b[12:16]) {
	case "VP8X":
		w := 1 + (int(b[24]) | int(b[25])<<8 | int(b[26])<<16)
		h := 1 + (int(b[27]) | int(b[28])<<8 | int(b[29])<<16)
		return w, h, true
	case "VP8 ":
		return int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff), true
	case "VP8L":
		v := binary.LittleEndian.Uint32(b[21:25])
		return int(v&0x3fff) + 1, int((v>>14)&0x3fff) + 1, true
	}
	return 0, 0, false
}
