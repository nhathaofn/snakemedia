package muse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/events"
	"SnakeMedia/internal/media"
	"SnakeMedia/internal/paths"
)

// TestLiveLongVideo asks Muse for videos longer than its 10-second clip, in
// one message, to learn which wording makes Muse cut the video into scenes
// and send back one joined file. Every case spends Muse turns (a 60-second
// case several). Run with SNAKEMEDIA_LIVE_LONG=1 (SNAKEMEDIA_LIVE_LONG_CASES
// picks cases by number, e.g. "2,3").
func TestLiveLongVideo(t *testing.T) {
	if os.Getenv("SNAKEMEDIA_LIVE_LONG") != "1" {
		t.Skip("set SNAKEMEDIA_LIVE_LONG=1 to spend Muse turns on the real accounts")
	}
	// Each case is the message the app sends (buildMessage) for a different
	// kind of video, so Muse cannot hand back clips made for an earlier case.
	cases := []MuseJob{
		{Ratio: "9:16", Duration: 60, Prompt: "bản tin công nghệ: nam MC giới thiệu chiếc điện thoại gập mới, cận cảnh sản phẩm, thử độ bền bản lề, công bố giá bán và ngày mở bán"},
		{Ratio: "16:9", Duration: 40, Prompt: "ông lão đánh cá ra khơi lúc bình minh, quăng lưới, cơn bão kéo đến, ông trở về với con gái đang đợi trên bãi biển. Có giọng kể chuyện"},
		{Ratio: "9:16", Duration: 30, Prompt: "A street dancer practices alone in an empty parking garage at night, misses a hard move, tries again and finally lands it. Upbeat hip-hop music, no voice."},
		{Ratio: "16:9", Duration: 60, Prompt: "phim tài liệu quân sự có thuyết minh giọng nam trầm: một đơn vị pháo binh hành quân đêm, triển khai trận địa trên đồi, khai hỏa lúc rạng sáng"},
		{Ratio: "9:16", Duration: 50, Prompt: "truyện kể đêm khuya, giọng nữ kể chậm rãi: cô sinh viên về quê ngủ lại căn nhà cũ của bà, nửa đêm nghe tiếng võng kẽo kẹt, sáng ra mới biết bà đã ru cô ngủ như hồi nhỏ"},
		{Ratio: "1:1", Duration: 20, Prompt: "quảng cáo nước hoa: chai thủy tinh màu hổ phách trên nền lụa đen, giọt nước hoa rơi, hoa nhài nở chậm, kết bằng chai nước hoa dưới ánh nến"},
	}
	// SNAKEMEDIA_LIVE_LONG_PROMPT (a text file) runs that prompt alone, at
	// SNAKEMEDIA_LIVE_LONG_SECONDS and SNAKEMEDIA_LIVE_LONG_RATIO.
	if file := os.Getenv("SNAKEMEDIA_LIVE_LONG_PROMPT"); file != "" {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		// SNAKEMEDIA_LIVE_LONG_SECONDS may list several lengths: one case each.
		cases = nil
		for _, s := range strings.Split(os.Getenv("SNAKEMEDIA_LIVE_LONG_SECONDS"), ",") {
			var seconds int
			fmt.Sscan(strings.TrimSpace(s), &seconds)
			cases = append(cases, MuseJob{Ratio: os.Getenv("SNAKEMEDIA_LIVE_LONG_RATIO"), Duration: seconds, Prompt: strings.TrimSpace(string(text))})
		}
	}
	for i := range cases {
		cases[i].Kind = MuseJobVideo
	}
	pick := map[int]bool{}
	for _, s := range strings.Split(os.Getenv("SNAKEMEDIA_LIVE_LONG_CASES"), ",") {
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
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Minute)
	defer cancel()
	accts := accounts.New(ps.AccountsDir, cfg, NewSite(cfg))
	accts.Start(ctx, func(events.Event, any) {})
	defer accts.Close()
	tmp := t.TempDir()
	out := os.Getenv("SNAKEMEDIA_LIVE_LONG_OUT")
	if out == "" {
		out = filepath.Join(tmp, "media")
	}
	store := media.NewStore(out)
	p := New(tmp, filepath.Join(tmp, "out"), cfg, store, accts)

	recs, err := accts.Records(accounts.ProviderMuse)
	if err != nil {
		t.Fatal(err)
	}
	var signedIn []accounts.Record
	for _, r := range recs {
		// SNAKEMEDIA_LIVE_LONG_SKIP leaves out accounts another run is using.
		// SNAKEMEDIA_LIVE_LONG_ONLY keeps just the listed accounts.
		only := os.Getenv("SNAKEMEDIA_LIVE_LONG_ONLY")
		if r.Enabled && r.Status == accounts.StatusSignedIn && !strings.Contains(os.Getenv("SNAKEMEDIA_LIVE_LONG_SKIP"), r.ID) && (only == "" || strings.Contains(only, r.ID)) {
			signedIn = append(signedIn, r)
		}
	}
	if len(signedIn) == 0 {
		t.Fatal("no signed-in Muse account")
	}
	t.Logf("%d signed-in Muse account(s)", len(signedIn))

	// One worker per account takes the next case, like the scheduler spreads
	// jobs; the cases run side by side.
	queue := make(chan int, len(cases))
	for n := range cases {
		if len(pick) == 0 || pick[n+1] {
			queue <- n
		}
	}
	close(queue)
	var wg sync.WaitGroup
	for w, rec := range signedIn {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer accts.Acquire(rec.ID)()
			page, err := p.openPage(ctx, rec)
			if err == nil {
				err = p.waitSignedIn(ctx, page, 2*time.Minute)
			}
			if err != nil {
				t.Logf("account %s: %v", rec.Label(), err)
				return
			}
			// SNAKEMEDIA_LIVE_LONG_SESSIONS re-reads earlier chats of the first
			// account (no turn spent).
			// SNAKEMEDIA_LIVE_LONG_ASK sends a question (no media asked) on the
			// first account and logs Muse's whole answer.
			// "session:<id>" re-reads that chat's answer instead.
			if q := os.Getenv("SNAKEMEDIA_LIVE_LONG_ASK"); w == 0 && strings.HasPrefix(q, "session:") {
				id := strings.TrimPrefix(q, "session:")
				evs, err := page.Result(ctx, id)
				var texts []string
				for _, e := range evs {
					texts = append(texts, e.Text)
				}
				t.Logf("ask: session %s (%v), answer:\n%s", id, err, strings.Join(texts, "\n"))
			} else if w == 0 && q != "" {
				if res, err := page.Send(ctx, q, nil); err == nil && res.OK {
					for start := time.Now(); time.Since(start) < 10*time.Minute; time.Sleep(4 * time.Second) {
						if s, _ := page.Status(ctx, res.SessionID); s == sessionCompleted || s == sessionInterrupted {
							break
						}
					}
					evs, _ := page.Result(ctx, res.SessionID)
					t.Logf("ask: session %s, answer:\n%s", res.SessionID, replyText(evs))
				} else {
					t.Logf("ask: %v %+v", err, res)
				}
			}
			if w == 0 {
				for _, id := range strings.Split(os.Getenv("SNAKEMEDIA_LIVE_LONG_SESSIONS"), ",") {
					if id = strings.TrimSpace(id); id != "" {
						longResult(ctx, t, p, page, store, cfg, "session "+id[:8], id, 0)
					}
				}
			}
			for n := range queue {
				res, err := page.Send(ctx, buildMessage(cases[n]), nil)
				if err != nil || !res.OK || res.SessionID == "" {
					t.Logf("case %d: send failed: %v %+v", n+1, err, res)
					continue
				}
				t.Logf("case %d: account %s, session %s", n+1, rec.Label(), res.SessionID)
				longResult(ctx, t, p, page, store, cfg, fmt.Sprintf("case %d", n+1), res.SessionID, cases[n].Duration)
			}
		}()
	}
	wg.Wait()
}

// longResult waits for a chat to end, as the app does: a turn ends only once
// the chat holds a newer reply, since right after a send into the chat its
// status may still be the last turn's "completed". An INCOMPLETE status gets
// continueMessage in the same chat, at most continueLimit times. It logs each
// reply's CHANGED and STATUS lines and downloads and measures every video.
func longResult(ctx context.Context, t *testing.T, p *Provider, page Page, store *media.Store, cfg *config.Manager, name, session string, asked int) {
	start := time.Now()
	var evs []AssistantEvent
	status := ""
	seen := 0
	for turn := 0; ; turn++ {
		for time.Since(start) < 60*time.Minute {
			s, err := page.Status(ctx, session)
			if err != nil {
				t.Logf("%s: status: %v", name, err)
			} else if s != status {
				t.Logf("%s: status %q after %s", name, s, time.Since(start).Round(time.Second))
			}
			if err == nil {
				status = s
				if s == sessionCompleted || s == sessionInterrupted {
					if e, err := page.Result(ctx, session); err == nil && len(e) > seen {
						break
					}
				}
			}
			time.Sleep(5 * time.Second)
		}
		var err error
		if evs, err = page.Result(ctx, session); err != nil {
			t.Logf("%s: result: %v", name, err)
			return
		}
		reply := repliesAfter(evs, seen)
		st := parseLongStatus(reply)
		t.Logf("%s: turn %d %s after %s, events %d->%d, status %q %q, changed %q; reply %q", name, turn+1, status,
			time.Since(start).Round(time.Second), seen, len(evs), st.State, st.Detail, st.Changed, truncate(strings.Join(strings.Fields(reply), " "), 700))
		seen = len(evs)
		if asked <= museClipSeconds || st.State != statusIncomplete || turn == continueLimit(MuseJob{Kind: MuseJobVideo, Duration: asked}) {
			break
		}
		cp, ok := page.(*chromePage)
		if !ok {
			break
		}
		var r SendResult
		if err := cp.invoke(ctx, cp.sendTimeout, "send", &r, continueMessage, []Attachment(nil), session); err != nil || !r.OK {
			t.Logf("%s: continue: %v %+v", name, err, r)
			break
		}
		t.Logf("%s: sent continue", name)
	}
	wanted, _ := wantedMedia(evs, MuseJobVideo)
	t.Logf("%s: %d video(s) in the chat", name, len(wanted))
	for k, m := range wanted {
		url, err := p.download(ctx, page, m, fmt.Sprintf("muse-%s-%d", strings.ReplaceAll(name, " ", "-"), k+1))
		if err != nil {
			t.Logf("%s video %d: download: %v (path %s, Muse said %d bytes, missing %v)", name, k+1, err, m.Path, m.ByteLen, m.Missing)
			// Again under the app's own cap, to see the file's real size.
			var n countWriter
			read, err := page.Download(ctx, m.Path, &n)
			t.Logf("%s video %d: raw read %d bytes, err %v", name, k+1, read, err)
			continue
		}
		file, _ := store.Path(url)
		t.Logf("%s video %d: %s (%s)", name, k+1, measure(ctx, file, cfg.Get().Flow), file)
	}
}

// countWriter counts the bytes written to it.
type countWriter int64

func (c *countWriter) Write(b []byte) (int, error) {
	*c += countWriter(len(b))
	return len(b), nil
}
