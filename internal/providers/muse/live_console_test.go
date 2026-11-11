package muse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"os/exec"
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

// consoleStep is one message of a TestLiveConsole script.
type consoleStep struct {
	// Name names the step's output files and lets a later step continue its chat.
	Name string `json:"name"`
	// Account is the index among the usable signed-in Muse accounts. A step
	// with Continue runs on the account of the step it continues.
	Account int `json:"account"`
	// Continue sends into the chat of that earlier step instead of a new chat.
	Continue string `json:"continue,omitempty"`
	// Text (or TextFile) is sent as is. Job builds the app's message instead.
	Text     string `json:"text,omitempty"`
	TextFile string `json:"textFile,omitempty"`
	Job      *struct {
		Kind        MuseJobKind   `json:"kind"`
		Ratio       string        `json:"ratio"`
		Duration    int           `json:"duration"`
		Prompt      string        `json:"prompt"`
		PromptFile  string        `json:"promptFile"`
		Frames      []string      `json:"frames"`
		SourceVideo string        `json:"sourceVideo"`
		RefInfo     []MuseRefInfo `json:"refInfo"`
	} `json:"job,omitempty"`
	// Images are attached in order (a Job's frames first, then these).
	Images []string `json:"images,omitempty"`
	// Media is the kind of file to download: "video", "image" or "" (none).
	Media string `json:"media,omitempty"`
}

// TestLiveConsole talks to Muse on real accounts of the dev data folder from a
// script, to study how Muse's agent handles a message: it sends each step (a
// new chat, or a follow-up in an earlier step's chat), waits for the answer
// and writes <name>.sent.txt, .reply.txt, .history.json (the raw chat events,
// tool calls included), the downloaded file and .meta.json into the output
// folder. Steps on different accounts run in parallel, steps on one account in
// order. With SNAKEMEDIA_FFMPEG it also writes a <name>.strip.png of frames.
// Every step may spend Muse quota.
//
//	SNAKEMEDIA_MUSE_CONSOLE=<script.json> go test ./internal/providers/muse -run '^TestLiveConsole$' -v -count=1 -timeout 3h
//
// Paths in the script are relative to its folder; output goes to its "out"
// sub-folder unless SNAKEMEDIA_MUSE_CONSOLE_OUT is set.
func TestLiveConsole(t *testing.T) {
	script := os.Getenv("SNAKEMEDIA_MUSE_CONSOLE")
	if script == "" {
		t.Skip("set SNAKEMEDIA_MUSE_CONSOLE to a script to spend Muse turns on the dev accounts")
	}
	base := filepath.Dir(script)
	at := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(base, p)
	}
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	var steps []consoleStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		t.Fatal(err)
	}
	out := os.Getenv("SNAKEMEDIA_MUSE_CONSOLE_OUT")
	if out == "" {
		out = filepath.Join(base, "out")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	ps, err := paths.Resolve("SnakeMedia")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewManager(ps.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	accts := accounts.New(ps.AccountsDir, cfg, NewSite(cfg))
	accts.Start(ctx, func(events.Event, any) {})
	defer accts.Close()
	store := media.NewStore(filepath.Join(out, "media"))
	p := New(t.TempDir(), filepath.Join(out, "saved"), cfg, store, accts)

	recs, err := accts.Records(accounts.ProviderMuse)
	if err != nil {
		t.Fatal(err)
	}
	only := os.Getenv("SNAKEMEDIA_MUSE_CONSOLE_ONLY")
	var pages []Page
	for _, r := range recs {
		if !r.Enabled || r.Status != accounts.StatusSignedIn || (only != "" && !strings.Contains(only, r.ID)) {
			continue
		}
		defer accts.Acquire(r.ID)()
		pg, err := p.openPage(ctx, r)
		if err == nil {
			err = p.waitSignedIn(ctx, pg, 2*time.Minute)
		}
		if err != nil {
			t.Logf("account %s skipped: %v", r.Label(), err)
			continue
		}
		t.Logf("account %d = %s (%s)", len(pages), r.Label(), r.ID)
		pages = append(pages, pg)
	}
	if len(pages) == 0 {
		t.Fatal("no usable signed-in Muse account")
	}

	var logMu sync.Mutex
	logf := func(format string, a ...any) {
		line := time.Now().Format("15:04:05 ") + fmt.Sprintf(format, a...)
		t.Log(line)
		logMu.Lock()
		defer logMu.Unlock()
		if f, err := os.OpenFile(filepath.Join(out, "console.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
	}

	// Each step's chat and account, for steps that continue it.
	type chat struct {
		account int
		session string
		seen    int
	}
	var chatMu sync.Mutex
	chats := map[string]chat{}
	byAccount := map[int][]consoleStep{}
	accountOf := map[string]int{}
	for _, s := range steps {
		if s.Continue != "" {
			s.Account = accountOf[s.Continue]
		}
		s.Account %= len(pages)
		accountOf[s.Name] = s.Account
		byAccount[s.Account] = append(byAccount[s.Account], s)
	}

	runStep := func(s consoleStep) {
		page := pages[s.Account]
		text := s.Text
		if s.TextFile != "" {
			b, err := os.ReadFile(at(s.TextFile))
			if err != nil {
				logf("%s: %v", s.Name, err)
				return
			}
			text = strings.ReplaceAll(string(b), "\r\n", "\n")
		}
		images := append([]string{}, s.Images...)
		if j := s.Job; j != nil {
			prompt := j.Prompt
			if j.PromptFile != "" {
				b, err := os.ReadFile(at(j.PromptFile))
				if err != nil {
					logf("%s: %v", s.Name, err)
					return
				}
				prompt = strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n"))
			}
			job := MuseJob{Kind: j.Kind, Ratio: j.Ratio, Duration: j.Duration, Prompt: prompt, RefInfo: j.RefInfo, Frames: j.Frames, References: s.Images, SourceVideo: j.SourceVideo}
			if len(j.Frames) > 0 {
				job.BoardFrames = len(j.Frames)
			}
			text = buildMessage(job)
			images = append(append([]string{}, j.Frames...), s.Images...)
			if j.SourceVideo != "" {
				images = append([]string{j.SourceVideo}, images...)
			}
		}
		var atts []Attachment
		for k, img := range images {
			data, err := os.ReadFile(at(img))
			if err != nil {
				logf("%s: %v", s.Name, err)
				return
			}
			ext := strings.ToLower(filepath.Ext(img))
			name, category := fmt.Sprintf("image_%d%s", k+1, ext), "image"
			if strings.HasPrefix(mime.TypeByExtension(ext), "video/") {
				name, category = "video_1"+ext, "video"
			}
			atts = append(atts, Attachment{Filename: name, MimeType: mime.TypeByExtension(ext), DataBase64: base64.StdEncoding.EncodeToString(data), Category: category})
		}
		_ = os.WriteFile(filepath.Join(out, s.Name+".sent.txt"), []byte(text), 0o644)

		start := time.Now()
		var res SendResult
		seen := 0
		if s.Continue != "" {
			chatMu.Lock()
			c, ok := chats[s.Continue]
			chatMu.Unlock()
			if !ok {
				logf("%s: no chat from %s", s.Name, s.Continue)
				return
			}
			seen = c.seen
			res, err = page.SendTo(ctx, c.session, text)
		} else {
			res, err = page.Send(ctx, text, atts)
		}
		if err != nil || !res.OK || res.SessionID == "" {
			logf("%s: send failed: %v %+v", s.Name, err, res)
			return
		}
		status := ""
		for time.Since(start) < 90*time.Minute {
			time.Sleep(6 * time.Second)
			st, err := page.Status(ctx, res.SessionID)
			if err != nil {
				continue
			}
			status = st
			if st == sessionCompleted || st == sessionInterrupted {
				break
			}
		}
		took := time.Since(start).Round(time.Second)
		if cp, ok := page.(*chromePage); ok {
			var hist json.RawMessage
			if err := cp.invoke(ctx, cp.callTimeout, "history", &hist, res.SessionID); err != nil {
				logf("%s: history: %v", s.Name, err)
			} else {
				_ = os.WriteFile(filepath.Join(out, s.Name+".history.json"), hist, 0o644)
			}
		}
		evs, err := page.Result(ctx, res.SessionID)
		if err != nil {
			logf("%s: result: %v", s.Name, err)
			return
		}
		chatMu.Lock()
		chats[s.Name] = chat{account: s.Account, session: res.SessionID, seen: len(evs)}
		chatMu.Unlock()
		_ = os.WriteFile(filepath.Join(out, s.Name+".reply.txt"), []byte(repliesAfter(evs, seen)), 0o644)

		meta := map[string]any{"session": res.SessionID, "status": status, "seconds": took.Seconds(), "chars": len(text), "attachments": len(atts)}
		if s.Media != "" {
			kind := MuseJobVideo
			if s.Media == "image" {
				kind = MuseJobImage
			}
			wanted, _ := wantedMedia(evs[seen:], kind)
			meta["files"] = len(wanted)
			if len(wanted) > 0 {
				url, err := p.download(ctx, page, wanted[len(wanted)-1], "console-"+s.Name)
				if err != nil {
					meta["download"] = err.Error()
				} else if file, err := store.Path(url); err == nil {
					meta["file"] = file
					meta["measure"] = measure(ctx, file, cfg.Get().Flow)
					if ff := os.Getenv("SNAKEMEDIA_FFMPEG"); ff != "" && kind == MuseJobVideo {
						strip := filepath.Join(out, s.Name+".strip.png")
						_ = exec.CommandContext(ctx, ff, "-v", "error", "-y", "-i", file, "-vf", "fps=0.5,scale=384:-1,tile=5x1", "-frames:v", "1", strip).Run()
					}
				}
			}
		}
		b, _ := json.MarshalIndent(meta, "", "  ")
		_ = os.WriteFile(filepath.Join(out, s.Name+".meta.json"), b, 0o644)
		logf("%s: %s after %s, %d chars, %v", s.Name, status, took, len(text), meta["measure"])
	}

	var wg sync.WaitGroup
	for _, list := range byAccount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, s := range list {
				runStep(s)
			}
		}()
		time.Sleep(15 * time.Second)
	}
	wg.Wait()
}
