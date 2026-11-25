package muse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/events"
)

func TestP1_M5_RetrySaveReleasesProviderLockDuringDiskCopyAndEmit(t *testing.T) {
	p := testProvider(t, &fakePage{})
	imgBytes := testPNGBytes(t, 32, 32)
	u, err := p.media.Put("m5-out", "image/png", ".png", imgBytes)
	if err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "batch-out")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p.media.AllowRoot(outDir)
	job := MuseJob{
		ID: "j-m5", Kind: MuseJobImage, Mode: MuseJobBatch, Prompt: "test",
		Status: MuseJobSucceeded, OutputDir: outDir, Index: 1,
		Files: []string{u}, SavedFiles: []string{}, OutputPaths: map[string]string{},
	}
	if _, err := p.jobs.add(job); err != nil {
		t.Fatal(err)
	}
	emitAcquiredLock := false
	p.emit = func(e events.Event, _ any) {
		if e == events.EventMuseJobUpdated {
			// If RetrySave still held p.mu here, TryLock would fail.
			if p.mu.TryLock() {
				emitAcquiredLock = true
				p.mu.Unlock()
			}
		}
	}
	res := p.RetrySave(job.ID)
	if !res.Success || len(res.Data.SavedFiles) != 1 {
		t.Fatalf("RetrySave = %+v", res)
	}
	if !emitAcquiredLock {
		t.Fatal("RetrySave held p.mu while saving outputs and emitting EventMuseJobUpdated")
	}
}

func TestP1_M6_M7_VaultNormalizeAndRoundTrip(t *testing.T) {
	if got := normalizeProject("_chung"); got != "" {
		t.Fatalf("normalizeProject(_chung) = %q, want empty", got)
	}
	if got := normalizeProject("_CHUNG"); got != "" {
		t.Fatalf("normalizeProject(_CHUNG) = %q, want empty", got)
	}
	if got := normalizeProject("Oliva Cottage"); got != "OLIVA_COTTAGE" {
		t.Fatalf("normalizeProject(Oliva Cottage) = %q", got)
	}

	built := BuildAssetFileName("Oliva Cottage", MuseTypeChar, "hero", "", "", "", 1, false, ".png")
	if built != "OLIVA_COTTAGE_CHAR_hero.png" {
		t.Fatalf("built = %q", built)
	}
	parsed := ParseAssetFileName(built)
	if parsed == nil || parsed.Project != "OLIVA_COTTAGE" || parsed.Type != MuseTypeChar || parsed.Alias != "hero" {
		t.Fatalf("round-trip multi-word project failed: %+v", parsed)
	}

	sv := ParseAssetFileName("CHAR_hero_wet_young_v2.png")
	if sv == nil || sv.State != "wet" || sv.Variant != "young" || sv.Version != 2 {
		t.Fatalf("state-before-variant misparsed: %+v", sv)
	}
}

func TestP1_M8_ComposePlusTagAndVersionOne(t *testing.T) {
	assets := []MuseAsset{
		{ID: "g1", Tag: "jimena+hester", Role: MuseRoleCharacter, Type: MuseTypeChar, Images: []string{"/flow-media/g.png"}},
		{
			ID: "h1", Tag: "hero", Role: MuseRoleCharacter, Type: MuseTypeChar,
			Items: []MuseAssetItem{
				{ID: "i1", URL: "/flow-media/h-v1.png", Version: 1},
				{ID: "i2", URL: "/flow-media/h-v2.png", Version: 2, Primary: true},
			},
		},
	}
	refs, _, tags, warns := resolvePromptRefs("Look at @jimena+hester and @hero_v1 together.", "", 0, nil, assets, nil, MuseRefShared)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(refs) != 2 || refs[0] != "/flow-media/g.png" || refs[1] != "/flow-media/h-v1.png" {
		t.Fatalf("refs = %v, tags = %v", refs, tags)
	}
}

func TestP1_M9_M10_M11_M12_AssetIntegrity(t *testing.T) {
	p := testProvider(t, &fakePage{})
	imgA, err := p.media.Put("a", "image/png", ".png", testPNGBytes(t, 24, 24))
	if err != nil {
		t.Fatal(err)
	}
	imgB, err := p.media.Put("b", "image/png", ".png", testPNGBytes(t, 24, 24))
	if err != nil {
		t.Fatal(err)
	}

	// M-9: my-hero and my_hero collide in assetScopeKey.
	first := p.SaveAsset(MuseAsset{
		Tag: "my-hero", Role: MuseRoleCharacter,
		Items: []MuseAssetItem{{URL: imgA, Primary: true}, {URL: imgB, Primary: true, Angle: "side"}},
	})
	if !first.Success {
		t.Fatal(first.Error)
	}
	// M-10a: only one Primary item remains active.
	primaries := 0
	for _, it := range first.Data.Items {
		if it.Primary {
			primaries++
		}
	}
	if primaries != 1 {
		t.Fatalf("expected 1 primary item, got %d: %+v", primaries, first.Data.Items)
	}
	dup := p.SaveAsset(MuseAsset{
		Tag: "my_hero", Role: MuseRoleCharacter,
		Items: []MuseAssetItem{{URL: imgB}},
	})
	if dup.Success {
		t.Fatal("my_hero should collide with my-hero")
	}

	// M-12a: UpdateAssetsRole updates Type and renames managed vault file prefix.
	upd := p.UpdateAssetsRole([]string{first.Data.ID}, MuseRoleSetting)
	if !upd.Success || upd.Data != 1 {
		t.Fatalf("UpdateAssetsRole = %+v", upd)
	}
	updated, ok := p.assets.Get(first.Data.ID)
	if !ok || updated.Role != MuseRoleSetting || updated.Type != MuseTypeLoc {
		t.Fatalf("Type not synced with Role: %+v", updated)
	}
	if !strings.HasPrefix(updated.Items[0].FileName, "LOC_my_hero") {
		t.Fatalf("vault filename not renamed to LOC_*: %q", updated.Items[0].FileName)
	}

	// M-10b: ImportAssetBatch with MergeByAlias merges Name and Lock from sibling rows.
	diskDir := t.TempDir()
	f1 := filepath.Join(diskDir, "f1.png")
	f2 := filepath.Join(diskDir, "f2.png")
	_ = os.WriteFile(f1, testPNGBytes(t, 16, 16), 0o600)
	_ = os.WriteFile(f2, testPNGBytes(t, 16, 16), 0o600)
	acc := addSignedIn(t, p, "CapAcc")
	withCaps(p, acc, 5)
	imp := p.ImportAssetBatch(MuseAssetBatchRequest{
		MergeByAlias: true,
		Items: []MuseAssetBatchItem{
			{SourcePath: f1, Tag: "mergedhero", Role: MuseRoleCharacter, Lock: "scar", Angle: "front"},
			{SourcePath: f2, Tag: "mergedhero", Name: "Display Name", Role: MuseRoleCharacter, Lock: "red coat", Angle: "side"},
		},
	})
	if !imp.Success || len(imp.Data.SavedIDs) != 1 {
		t.Fatalf("ImportAssetBatch = %+v", imp)
	}
	merged, _ := p.assets.Get(imp.Data.SavedIDs[0])
	if merged.Name != "Display Name" || !strings.Contains(merged.Lock, "scar") || !strings.Contains(merged.Lock, "red coat") {
		t.Fatalf("merged metadata lost: %+v", merged)
	}
}

func TestP1_M13_LongVideoUnicodeTailAndChangedNone(t *testing.T) {
	st := parseLongStatus("STATUS: INCOMPLETE không thể tạo thêm\nCHANGED: none.")
	if st.State != statusIncomplete || st.Detail != "không thể tạo thêm" || st.Changed != "" {
		t.Fatalf("misparsed long status: %+v", st)
	}
	st2 := parseLongStatus("STATUS: DONE.\nCHANGED: changed lighting.")
	if st2.State != statusDone || st2.Changed != "changed lighting" {
		t.Fatalf("misparsed normal status: %+v", st2)
	}
}

func TestP1_M16_InspectSkipVideoCheckAndPausedUntilPreserved(t *testing.T) {
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	page := &fakePage{
		loggedIn: true,
		quota:    &MuseQuota{PercentUsed: 10, QuotaStatus: "daily limit reached"},
	}
	site, _ := testSite(t, page)
	site.now = func() time.Time { return now }

	// SkipVideoCheck (image-only) should not 24h-disable account and should resume restDailyVideo.
	dataRaw, _ := encodeMuseData(museData{RestReason: restDailyVideo})
	recImgOnly := accounts.Record{
		Account: accounts.Account{ID: "img-only", SkipVideoCheck: true, Enabled: false},
		Data:    dataRaw,
	}
	inspImgOnly, err := site.Inspect(context.Background(), nil, recImgOnly)
	if err != nil || inspImgOnly.DisableAccount || !inspImgOnly.Resume {
		t.Fatalf("SkipVideoCheck inspect = %+v, err=%v", inspImgOnly, err)
	}

	// Non-SkipVideoCheck re-check preserves existing future PausedUntil rather than pushing +24h forward.
	existingUntil := now.Add(4 * time.Hour)
	recPaused := accounts.Record{
		Account: accounts.Account{ID: "paused", PausedUntil: existingUntil.Format(time.RFC3339)},
		Data:    dataRaw,
	}
	inspPaused, err := site.Inspect(context.Background(), nil, recPaused)
	if err != nil || !inspPaused.PausedUntil.Equal(existingUntil) {
		t.Fatalf("PausedUntil shifted from %v to %v (err=%v)", existingUntil, inspPaused.PausedUntil, err)
	}
}

func TestP1_M17_M18_MultiWordNodeZeroLimitAndInUseRefreshClear(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	_ = p.assets.Add(MuseAsset{ID: "id-mw", Tag: "mwo", Name: "Two Words Name", Role: MuseRoleProp, Images: []string{"/flow-media/a.png"}})

	ids, err := p.resolveNodeAssets(engine.Texts{"Two Words Name"})
	if err != nil || len(ids) != 1 || ids[0] != "id-mw" {
		t.Fatalf("resolveNodeAssets(multi-word) = (%v, %v)", ids, err)
	}

	if _, sent, noteErr := sendNote(context.Background(), &fakePage{loggedIn: true}, "hello", 0, p.cfg.Get().Muse, noSleep, time.Now); noteErr == nil || sent {
		t.Fatalf("sendNote limit=0 sent=%v err=%v, want immediate non-sent error", sent, noteErr)
	}

	// Simulate cleanup error wording without "thiết lập".
	cleanErrPage := &fakePage{loggedIn: true, sendRes: SendResult{OK: true, SessionID: "c1"}, statusErr: errors.New("socket down")}
	cfg := p.cfg.Get().Muse
	cfg.PollErrorLimit = 1
	if _, _, cErr := sendNoteWithLabel(context.Background(), cleanErrPage, "clean", "dọn chat", time.Minute, cfg, noSleep, time.Now); cErr == nil || strings.Contains(cErr.Error(), "thiết lập") {
		t.Fatalf("cleanup sendNote leaked setup wording: %v", cErr)
	}

	// M-18: Clear stateRefreshSeen when InUse early return occurs.
	acc := addSignedIn(t, p, "BusyRefresh")
	busyRelease := p.accounts.Acquire(acc.ID)
	p.scheduleStateAccountRefresh(acc, "queue_needs_account")
	p.wg.Wait()
	busyRelease()
	p.mu.Lock()
	_, stale := p.acct.stateRefreshSeen[acc.ID]
	p.mu.Unlock()
	if stale {
		t.Fatal("stateRefreshSeen retained fingerprint after InUse early exit")
	}
}

func TestP1_M19_FormatFramesPreviewStoryboardPartialAndMeasureAllFiles(t *testing.T) {
	p := testProvider(t, &fakePage{loggedIn: true})
	acc := addSignedIn(t, p, "A")
	withCaps(p, acc, 6)

	// (a) buildMessage with Frames > 0 includes "not in the shape of the attached images".
	msg := buildMessage(MuseJob{Kind: MuseJobVideo, Ratio: "16:9", Frames: []string{"/flow-media/f1.png"}})
	if !strings.Contains(msg, "not in the shape of the attached images") {
		t.Fatalf("formatLines ignored Frames: %q", msg)
	}

	// (b) PreviewMessages counts storyboard frames in EstimatedBytes.
	noSB := p.PreviewMessages(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"walk"}})
	withSB := p.PreviewMessages(MuseJobRequest{Kind: MuseJobVideo, Prompts: []string{"walk"}, Storyboard: &MuseStoryboard{Frames: 4}})
	if !noSB.Success || !withSB.Success || withSB.Data[0].EstimatedBytes-noSB.Data[0].EstimatedBytes < 4*(240<<10) {
		t.Fatalf("storyboard bytes missing from estimate: noSB=%+v withSB=%+v", noSB, withSB)
	}

	// (c) Partial storyboard fail in releaseWaiting.
	board := MuseJob{ID: "b-partial", Kind: MuseJobImage, Status: MuseJobSucceeded, Files: []string{"/flow-media/only1.png"}, BoardFrames: 3}
	vid := MuseJob{ID: "v-partial", Kind: MuseJobVideo, Status: MuseJobQueued, After: "b-partial", BoardFrames: 3}
	_, _ = p.jobs.add(board, vid)
	p.releaseWaiting()
	gotVid := mustJob(t, p, "v-partial")
	if gotVid.Status != MuseJobFailed || !strings.Contains(gotVid.Error, "1/3") {
		t.Fatalf("partial storyboard did not fail cleanly: %+v", gotVid)
	}

	// (d) measureNote checks all files beyond files[0].
	u1, _ := p.media.Put("ok-ratio", "image/png", ".png", testPNGBytes(t, 100, 100))
	u2, _ := p.media.Put("bad-ratio", "image/png", ".png", testPNGBytes(t, 200, 100))
	note := p.measureNote(context.Background(), MuseJob{Kind: MuseJobImage, Ratio: "1:1"}, []string{u1, u2})
	if !strings.Contains(note, "File 2") || !strings.Contains(note, "200×100") {
		t.Fatalf("measureNote ignored mismatch on files[1]: %q", note)
	}
}

func TestP1_M20_ExifOrientationNormalizeAndURLSanitize(t *testing.T) {
	// Build a minimal 4x2 landscape JPEG whose EXIF Orientation tag 6 means 90deg CW -> 2x4 portrait after decode.
	src := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := range 20 {
		for x := range 40 {
			src.Set(x, y, color.NRGBA{R: 200, G: 50, B: 50, A: 255})
		}
	}
	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	rawJPG := injectExifOrientation(t, jpgBuf.Bytes(), 6)

	if !needsOutgoingJPEGNorm(rawJPG, ".jpg", 1<<20, testCaps()) {
		t.Fatal("needsOutgoingJPEGNorm must normalize EXIF-rotated small JPEG")
	}
	att := Attachment{
		Category:   "image",
		Filename:   "photo.jpg",
		MimeType:   "image/jpeg",
		DataBase64: base64.StdEncoding.EncodeToString(rawJPG),
	}
	out, err := reduceAttachment(att, 1<<20, testCaps())
	if err != nil {
		t.Fatal(err)
	}
	decodedBytes, _ := base64.StdEncoding.DecodeString(out.DataBase64)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(decodedBytes))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 20 || cfg.Height != 40 {
		t.Fatalf("expected rotated 20x40 portrait after EXIF orientation 6, got %dx%d", cfg.Width, cfg.Height)
	}

	// Sanitize away URL strips query tokens cleanly.
	if got := sanitizeAwayURL("https://auth.example.com/cb?token=SECRET_123#frag"); strings.Contains(got, "SECRET_123") {
		t.Fatalf("sanitizeAwayURL leaked token: %q", got)
	}
}

func injectExifOrientation(t *testing.T, jpegData []byte, orient uint16) []byte {
	t.Helper()
	// APP1 Exif payload: "Exif\x00\x00" + TIFF "MM" + 0x002A + off(8) + 1 entry (0x0112, SHORT=3, count=1, val=orient) + nextIFD(0)
	var tiff bytes.Buffer
	tiff.WriteString("MM")
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x002a))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(&tiff, binary.BigEndian, orient)
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0))

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var app1 bytes.Buffer
	app1.Write([]byte{0xff, 0xe1})
	_ = binary.Write(&app1, binary.BigEndian, uint16(2+len(payload)))
	app1.Write(payload)

	var out bytes.Buffer
	out.Write(jpegData[:2])
	out.Write(app1.Bytes())
	out.Write(jpegData[2:])
	return out.Bytes()
}

func encodeMuseData(d museData) ([]byte, error) {
	return json.Marshal(d)
}
