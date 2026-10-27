package muse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
)

// TestLiveFormatWording sends Muse a ratio/duration line in front of a prompt
// on the first signed-in account of the real data folder, downloads what
// comes back and logs its real size and length, to learn whether Muse follows
// that line. Each case spends one Muse turn. Run with SNAKEMEDIA_LIVE_FORMAT=1
// (SNAKEMEDIA_LIVE_FORMAT_CASES picks cases by number, e.g. "1,4").
func TestLiveFormatWording(t *testing.T) {
	if os.Getenv("SNAKEMEDIA_LIVE_FORMAT") != "1" {
		t.Skip("set SNAKEMEDIA_LIVE_FORMAT=1 to spend Muse turns on the real accounts")
	}
	const imagePrompt = "một tách cà phê nóng trên bàn gỗ cạnh cửa sổ, ánh sáng buổi sáng"
	const videoPrompt = "khói bốc lên từ tách cà phê nóng trên bàn gỗ, máy quay tiến chậm"
	cases := []struct {
		kind MuseJobKind
		text string
	}{
		{MuseJobImage, "Tạo ảnh · tỉ lệ 9:16 (dọc)\n" + imagePrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 16:9 (ngang)\n" + imagePrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 4:5\n" + imagePrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 9:16 (dọc) · dài 5 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 16:9 (ngang) · dài 10 giây\n" + videoPrompt},
		{MuseJobImage, "Tạo ảnh\n" + imagePrompt},
		{MuseJobVideo, "Tạo video\n" + videoPrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 1:1 (vuông)\n" + imagePrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 1:1 (vuông) · dài 5 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 9:16 (dọc) · dài 8 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 9:16 (dọc) · dài 15 giây\n" + videoPrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 2:3 (dọc)\n" + imagePrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 3:2 (ngang)\n" + imagePrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 3:4 (dọc)\n" + imagePrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 4:3 (ngang)\n" + imagePrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 5:4 (ngang)\n" + imagePrompt},
		{MuseJobImage, "Tạo ảnh · tỉ lệ 21:9 (ngang)\n" + imagePrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 4:3 (ngang) · dài 3 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 3:4 (dọc) · dài 4 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 21:9 (ngang) · dài 6 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 9:16 (dọc) · dài 2 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 16:9 (ngang) · dài 7 giây\n" + videoPrompt},
		{MuseJobVideo, "Tạo video · tỉ lệ 1:1 (vuông) · dài 9 giây\n" + videoPrompt},
	}
	pick := map[int]bool{}
	for _, s := range strings.Split(os.Getenv("SNAKEMEDIA_LIVE_FORMAT_CASES"), ",") {
		var n int
		if _, err := fmt.Sscan(strings.TrimSpace(s), &n); err == nil {
			pick[n] = true
		}
	}

	ps, err := paths.Resolve("SnakeMedia")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(ps.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	accts := accounts.New(ps.AccountsDir, cfg, NewSite(cfg))
	accts.Start(ctx, func(events.Event, any) {})
	defer accts.Close()
	tmp := t.TempDir()
	store := media.NewStore(filepath.Join(tmp, "media"))
	p := New(tmp, filepath.Join(tmp, "out"), cfg, store, accts)

	recs, err := accts.Records(accounts.ProviderMuse)
	if err != nil {
		t.Fatal(err)
	}
	var rec *accounts.Record
	for i := range recs {
		if recs[i].Enabled && recs[i].Status == accounts.StatusSignedIn {
			rec = &recs[i]
			break
		}
	}
	if rec == nil {
		t.Fatal("no signed-in Muse account")
	}
	// Held like a running job holds it, so the idle reaper leaves it open.
	defer accts.Acquire(rec.ID)()
	page, err := p.openPage(ctx, *rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.waitSignedIn(ctx, page, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	t.Logf("account %s", rec.Label())

	for n, c := range cases {
		if len(pick) > 0 && !pick[n+1] {
			continue
		}
		first := strings.SplitN(c.text, "\n", 2)[0]
		res, err := page.Send(ctx, c.text, nil)
		if err != nil || !res.OK || res.SessionID == "" {
			t.Logf("case %d %q: send failed: %v %+v", n+1, first, err, res)
			continue
		}
		start := time.Now()
		status := ""
		for time.Since(start) < 30*time.Minute {
			time.Sleep(4 * time.Second)
			s, err := page.Status(ctx, res.SessionID)
			if err != nil {
				t.Logf("case %d: status: %v", n+1, err)
				continue
			}
			status = s
			if s == sessionCompleted || s == sessionInterrupted {
				break
			}
		}
		events, err := page.Result(ctx, res.SessionID)
		if err != nil {
			t.Logf("case %d %q: result: %v", n+1, first, err)
			continue
		}
		wanted, _ := wantedMedia(events, c.kind)
		reply := replyText(events)
		if len(wanted) == 0 {
			t.Logf("case %d %q: %s after %s, no file; reply %q", n+1, first, status, time.Since(start).Round(time.Second), reply)
			continue
		}
		url, err := p.download(ctx, page, wanted[0], fmt.Sprintf("muse-format-%d", n+1))
		if err != nil {
			t.Logf("case %d %q: download: %v", n+1, first, err)
			continue
		}
		file, _ := store.Path(url)
		t.Logf("case %d %q: %s after %s, %d file(s), first %s; reply %q",
			n+1, first, status, time.Since(start).Round(time.Second), len(wanted), measure(ctx, file, cfg.Get().Flow), truncate(reply, 160))
		time.Sleep(20 * time.Second) // the app's own gap between two sends
	}
}

// measure describes a downloaded file's size (and a video's length).
func measure(ctx context.Context, file string, policy config.FlowConfig) string {
	if strings.EqualFold(filepath.Ext(file), ".mp4") {
		meta, err := media.ProbeVideo(ctx, file, policy)
		if err != nil {
			return "video: " + err.Error()
		}
		return fmt.Sprintf("video %dx%d (%s) %.1fs", meta.Width, meta.Height, ratio(meta.Width, meta.Height), meta.DurationSeconds)
	}
	w, h, err := media.ImageSize(file)
	if err != nil {
		return "image: " + err.Error()
	}
	return fmt.Sprintf("image %dx%d (%s)", w, h, ratio(w, h))
}

// ratio names w:h in lowest terms.
func ratio(w, h int) string {
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return "?"
	}
	return fmt.Sprintf("%d:%d", w/a, h/a)
}
