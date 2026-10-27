package main

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
)

// museWatchInstall puts a recorder on Muse's gateway runtime (every onEvent
// callback, summarised) and a sender (Muse's own chat client, new thread).
const museWatchInstall = `(() => {
  if (window.__mw) return true;
  const rt = () => window.__hatchEarlyGatewayRuntimeState.activeRuntime.getSnapshot();
  const chat = () => {
    let root;
    for (const el of [document.body, ...document.querySelectorAll("body *")].slice(0, 80)) {
      const k = Object.keys(el).find((k) => k.startsWith("__reactContainer") || k.startsWith("__reactFiber"));
      if (k) { root = el[k]; break; }
    }
    if (!root) return null;
    if (root.stateNode?.current) root = root.stateNode.current;
    const stack = [root];
    for (let n = 0; stack.length && n < 300000; n++) {
      const f = stack.pop();
      const v = f.memoizedProps?.value;
      if (v && typeof v.sendMessage === "function" && typeof v.ensureActiveChatSendReady === "function") return v;
      if (f.child) stack.push(f.child);
      if (f.sibling) stack.push(f.sibling);
    }
    return null;
  };
  const KEEP = /path|uri|byte|missing|status|state|phase|stage|activ|kind|type|name|event|role|tool|step|error|reason|refus|block|safety|moderat|finish|stop|session|seq|mime|uri|label|title|progress|percent/i;
  const brief = (v, depth = 0) => {
    if (Array.isArray(v)) return v.slice(0, 4).map((x) => brief(x, depth + 1));
    if (!v || typeof v !== "object") return typeof v === "string" ? v.slice(0, 120) : v;
    if (depth > 6) return "{…}";
    const o = {};
    for (const [k, x] of Object.entries(v)) {
      if (x && typeof x === "object") o[k] = brief(x, depth + 1);
      else if (KEEP.test(k)) o[k] = typeof x === "string" ? x.slice(0, 120) : x;
    }
    return o;
  };
  const log = [];
  const t0 = Date.now();
  let subscribed = "no";
  try {
    const r = rt();
    const off = r.onEvent((...args) => log.push({ t: Date.now() - t0, args: args.map((a) => brief(a)) }));
    subscribed = typeof off;
  } catch (e) { subscribed = "error: " + e.message; }
  window.__mw = {
    subscribed,
    async send(text) {
      const c = chat();
      if (!c) return { ok: false, reason: "no chat" };
      const r = await c.sendMessage(text, [], { createNewThread: true, waitForReadiness: true });
      return { ok: !!r?.ok, sessionId: r?.sessionId || "", reason: String(r?.reason || r?.error || "") };
    },
    async tick(sid) {
      const list = await rt().sendRequest("sessions.list", {});
      const s = (list.sessions || []).find((x) => x.session_id === sid);
      const events = log.splice(0);
      const panel = [...document.querySelectorAll("[data-testid^='hatch-status'],[data-testid*='activity']")].map((e) => e.dataset.testid + ": " + (e.innerText || "").replace(/\s+/g, " ").slice(0, 160));
      return { t: Date.now() - t0, session: s ? brief(s) : null, events, panel };
    },
    async history(sid) {
      const h = await rt().sendRequest("chat.history", { session_id: sid, limit: 200 });
      return (h.chat_events || []).map((e) => ({ ...brief(e), text: (e.display_text || "").slice(0, 300) }));
    },
  };
  return window.__mw.subscribed;
})()`

// liveMuseWatch sends one prompt per SNAKEMEDIA_LIVE_WATCH line (default: one
// harmless image) on the first signed-in Muse account and logs, every 3s,
// the session's list entry, every runtime event and the status panel, until
// the session has ended; then the chat history. It spends one Muse turn per
// prompt.
func liveMuseWatch(t *testing.T, a *App) {
	prompts := []string{"Tạo ảnh\na small red paper boat on a calm lake, morning light"}
	if env := os.Getenv("SNAKEMEDIA_LIVE_WATCH"); env != "" {
		prompts = strings.Split(env, "|")
	}
	recs, err := a.accounts.Records(accounts.ProviderMuse)
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
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	defer cancel()
	b, err := a.accounts.Browser(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	owns := func(u *url.URL) bool { return strings.HasSuffix(u.Hostname(), "muse.ai") }
	eval := func(expr string, out any) error {
		tab, _, err := browser.FindTab(ctx, b.Port, a.cfg.Get().Accounts, owns)
		if err != nil {
			return err
		}
		conn, err := browser.Dial(ctx, tab, b.Port, 64<<20)
		if err != nil {
			return err
		}
		defer conn.Close()
		return conn.Evaluate(ctx, "(async()=>JSON.stringify(await ("+expr+")))()", out)
	}
	var sub any
	for i := 0; ; i++ {
		if err = eval(museWatchInstall, &sub); err == nil {
			break
		}
		if i == 20 {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
	}
	t.Logf("onEvent subscribe: %v", sub)
	for _, prompt := range prompts {
		prompt = strings.ReplaceAll(prompt, `\n`, "\n")
		var sent struct {
			OK        bool   `json:"ok"`
			SessionID string `json:"sessionId"`
			Reason    string `json:"reason"`
		}
		arg, _ := json.Marshal(prompt)
		if err := eval("window.__mw.send("+string(arg)+")", &sent); err != nil || !sent.OK {
			t.Fatalf("send: %v %+v", err, sent)
		}
		t.Logf("=== sent %q session=%s", prompt, sent.SessionID)
		sid, _ := json.Marshal(sent.SessionID)
		ended := time.Time{}
		for start := time.Now(); time.Since(start) < 25*time.Minute; {
			var tick map[string]any
			if err := eval("window.__mw.tick("+string(sid)+")", &tick); err != nil {
				t.Logf("tick error: %v", err)
			} else {
				t.Logf("tick %s", dump(tick))
				if s, ok := tick["session"].(map[string]any); ok && s["status"] != "running" && ended.IsZero() && time.Since(start) > 10*time.Second {
					ended = time.Now()
				}
			}
			if !ended.IsZero() && time.Since(ended) > 15*time.Second {
				break
			}
			time.Sleep(3 * time.Second)
		}
		var hist any
		if err := eval("window.__mw.history("+string(sid)+")", &hist); err != nil {
			t.Logf("history error: %v", err)
		}
		t.Logf("history %s", dump(hist))
	}
}
