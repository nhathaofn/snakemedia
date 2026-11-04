package muse

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"
)

func noisyAttachment(t *testing.T, transparent bool) Attachment {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 800))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = byte(rng.Uint32()), byte(rng.Uint32()), byte(rng.Uint32())
		img.Pix[i+3] = 255
		if transparent {
			img.Pix[i+3] = 100
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return Attachment{Filename: "image_1.png", MimeType: "image/png", Category: "image", DataBase64: base64.StdEncoding.EncodeToString(data.Bytes())}
}

func compressionCaps() *MuseCapabilities {
	return &MuseCapabilities{MaxFileBytes: 20 << 20, MaxFiles: 10, ImageTypes: []MuseFileType{
		{Mime: "image/png", Extensions: []string{".png"}},
		{Mime: "image/jpeg", Extensions: []string{".jpg"}},
	}}
}

func TestPrepareAttachmentsFitsFullMessageAndPreservesOriginals(t *testing.T) {
	att := noisyAttachment(t, false)
	var atts []Attachment
	for i := range 6 {
		a := att
		a.Filename = fmt.Sprintf("image_%d.png", i+1)
		atts = append(atts, a)
	}
	job := MuseJob{Kind: MuseJobVideo, Duration: 30, Ratio: "9:16", Prompt: strings.Repeat("Cảnh quay có lời thoại tiếng Việt. ", 400), References: make([]string, 6)}
	message := buildMessage(job)
	if inlineRequestBytes(message, atts) <= museInlineRequestBudget {
		t.Fatal("fixture must exceed the request budget")
	}
	out, err := prepareAttachments(job, atts, compressionCaps())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 6 || inlineRequestBytes(message, out) > museInlineRequestBudget {
		t.Fatal("prepared request exceeds budget or dropped references")
	}
	for i, a := range out {
		if atts[i].DataBase64 != att.DataBase64 || atts[i].MimeType != "image/png" {
			t.Fatal("mutated original attachment")
		}
		if a.Filename != fmt.Sprintf("image_%d.jpg", i+1) || a.MimeType != "image/jpeg" {
			t.Fatal("reference order/name or MIME mismatch")
		}
		data, err := base64.StdEncoding.DecodeString(a.DataBase64)
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "jpeg" || cfg.Width*2 != cfg.Height*3 {
			t.Fatalf("invalid image/ratio: %+v %s %v", cfg, format, err)
		}
	}
	if message != buildMessage(job) {
		t.Fatal("changed the complete prompt")
	}
}

func TestPrepareAttachmentsLeavesSmallRequestUntouched(t *testing.T) {
	atts := []Attachment{{Filename: "image_1.png", MimeType: "image/png", Category: "image", DataBase64: base64.StdEncoding.EncodeToString([]byte("small original"))}}
	out, err := prepareAttachments(MuseJob{Kind: MuseJobImage, Prompt: "p"}, atts, compressionCaps())
	if err != nil || len(out) != 1 || out[0] != atts[0] {
		t.Fatalf("small request changed: %+v %v", out, err)
	}
}

func TestReduceAttachmentPreservesTransparencyAndPerFileLimit(t *testing.T) {
	att := noisyAttachment(t, true)
	caps := compressionCaps()
	caps.MaxFileBytes = 200 << 10
	out, err := reduceAttachment(att, 300<<10, caps)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(out.DataBase64)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "png" || out.MimeType != "image/png" || int64(len(data)) > caps.MaxFileBytes {
		t.Fatalf("invalid transparent output: %s %v", format, err)
	}
	c := color.NRGBAModel.Convert(img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2)).(color.NRGBA)
	if c.A == 255 {
		t.Fatal("lost transparency")
	}
}

func TestPrepareAttachmentsRejectsOversizedNonImageContent(t *testing.T) {
	for _, tc := range []struct {
		text string
		atts []Attachment
	}{
		{strings.Repeat("x", museInlineRequestBudget), nil},
		{"p", []Attachment{{Category: "video", DataBase64: strings.Repeat("A", museInlineRequestBudget)}}},
	} {
		_, err := prepareAttachments(MuseJob{Kind: MuseJobVideo, Prompt: tc.text}, tc.atts, compressionCaps())
		if _, ok := err.(*inputError); !ok {
			t.Fatalf("expected actionable input error, got %v", err)
		}
	}
}
