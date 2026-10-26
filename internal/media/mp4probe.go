package media

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
)

// maxMP4Box bounds how much of one metadata box is read into memory. moov
// holds only indexes and headers, never media samples.
const maxMP4Box = 64 << 20

// probeMP4 reads width, height and duration from the moov box of an MP4, MOV
// or M4V file, so the common formats need no ffprobe. Rotation from the track
// matrix swaps width and height like ffprobe's side data does.
func probeMP4(file string) (VideoMetadata, error) {
	f, err := os.Open(file)
	if err != nil {
		return VideoMetadata{}, err
	}
	defer f.Close()
	moov, err := findTopBox(f, "moov")
	if err != nil {
		return VideoMetadata{}, err
	}
	var meta VideoMetadata
	for _, box := range childBoxes(moov) {
		switch box.kind {
		case "mvhd":
			meta.DurationSeconds = mvhdDuration(box.body)
		case "trak":
			if width, height, ok := videoTrackSize(box.body); ok && meta.Width == 0 {
				meta.Width, meta.Height = width, height
			}
		}
	}
	if meta.Width <= 0 || meta.Height <= 0 || meta.DurationSeconds <= 0 || math.IsNaN(meta.DurationSeconds) || math.IsInf(meta.DurationSeconds, 0) {
		return VideoMetadata{}, errors.New("không đọc được thông số video từ tệp MP4")
	}
	return meta, nil
}

type mp4Box struct {
	kind string
	body []byte
}

// findTopBox scans top-level boxes and returns the body of the first one of kind.
func findTopBox(r io.ReadSeeker, kind string) ([]byte, error) {
	header := make([]byte, 16)
	for {
		if _, err := io.ReadFull(r, header[:8]); err != nil {
			return nil, errors.New("tệp không có box " + kind)
		}
		size := uint64(binary.BigEndian.Uint32(header[:4]))
		name := string(header[4:8])
		headerLen := uint64(8)
		if size == 1 {
			if _, err := io.ReadFull(r, header[8:16]); err != nil {
				return nil, err
			}
			size, headerLen = binary.BigEndian.Uint64(header[8:16]), 16
		}
		if size != 0 && size < headerLen {
			return nil, errors.New("box MP4 không hợp lệ")
		}
		if name == kind {
			if size == 0 || size-headerLen > maxMP4Box {
				return nil, errors.New("box " + kind + " quá lớn")
			}
			body := make([]byte, size-headerLen)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, err
			}
			return body, nil
		}
		if size == 0 {
			return nil, errors.New("tệp không có box " + kind)
		}
		if _, err := r.Seek(int64(size-headerLen), io.SeekCurrent); err != nil {
			return nil, err
		}
	}
}

// childBoxes splits a container body into its boxes, stopping at malformed data.
func childBoxes(body []byte) []mp4Box {
	var boxes []mp4Box
	for len(body) >= 8 {
		size := uint64(binary.BigEndian.Uint32(body[:4]))
		headerLen := uint64(8)
		if size == 1 && len(body) >= 16 {
			size, headerLen = binary.BigEndian.Uint64(body[8:16]), 16
		}
		if size == 0 {
			size = uint64(len(body))
		}
		if size < headerLen || size > uint64(len(body)) {
			break
		}
		boxes = append(boxes, mp4Box{kind: string(body[4:8]), body: body[headerLen:size]})
		body = body[size:]
	}
	return boxes
}

func findChild(body []byte, kind string) []byte {
	for _, box := range childBoxes(body) {
		if box.kind == kind {
			return box.body
		}
	}
	return nil
}

// mvhdDuration is the movie duration in seconds (version 0 or 1 header).
func mvhdDuration(b []byte) float64 {
	if len(b) < 1 {
		return 0
	}
	switch b[0] {
	case 0:
		if len(b) < 20 {
			return 0
		}
		scale := binary.BigEndian.Uint32(b[12:16])
		if scale == 0 {
			return 0
		}
		return float64(binary.BigEndian.Uint32(b[16:20])) / float64(scale)
	case 1:
		if len(b) < 32 {
			return 0
		}
		scale := binary.BigEndian.Uint32(b[20:24])
		if scale == 0 {
			return 0
		}
		return float64(binary.BigEndian.Uint64(b[24:32])) / float64(scale)
	}
	return 0
}

// videoTrackSize returns the display size of a video track, rotated by its matrix.
func videoTrackSize(trak []byte) (int, int, bool) {
	mdia := findChild(trak, "mdia")
	hdlr := findChild(mdia, "hdlr")
	if len(hdlr) < 12 || string(hdlr[8:12]) != "vide" {
		return 0, 0, false
	}
	tkhd := findChild(trak, "tkhd")
	if len(tkhd) < 1 {
		return 0, 0, false
	}
	// Matrix and size sit after the version-specific times: 76/88 bytes in.
	offset := 76
	if tkhd[0] == 1 {
		offset = 88
	}
	if len(tkhd) < offset+8 {
		return 0, 0, false
	}
	matrix := tkhd[offset-36 : offset]
	width := int(binary.BigEndian.Uint32(tkhd[offset:offset+4]) >> 16)
	height := int(binary.BigEndian.Uint32(tkhd[offset+4:offset+8]) >> 16)
	// a=0, b=±1 means a 90° or 270° rotation.
	if a, b := int32(binary.BigEndian.Uint32(matrix[0:4])), int32(binary.BigEndian.Uint32(matrix[4:8])); a == 0 && b != 0 {
		width, height = height, width
	}
	return width, height, width > 0 && height > 0
}
