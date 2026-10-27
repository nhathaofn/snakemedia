package main

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/browser"
)

// museProbeScript reads, without sending anything, how Muse reports a chat's
// progress: the session list's fields and state values, the event kinds of
// the latest chats (field names and short status-like values, no message
// text), the gateway runtime's methods, and the state words Muse's own client
// code uses near those calls. It is a research aid for the session-state table.
const museProbeScript = `(async () => {
  const out = {};
  const rt = window.__hatchEarlyGatewayRuntimeState?.activeRuntime?.getSnapshot?.();
  if (!rt) return { error: "no gateway runtime", path: location.pathname };
  const methods = (o) => { const s = new Set(); for (let x = o; x && x !== Object.prototype; x = Object.getPrototypeOf(x)) Object.getOwnPropertyNames(x).forEach((k) => { try { if (typeof o[k] === "function") s.add(k); } catch (e) {} }); return [...s].sort(); };
  out.runtimeKeys = Object.keys(rt).sort();
  out.runtimeMethods = methods(rt);
  const STATE = /status|state|phase|stage|activ|running|busy|progress|step|error|reason|refus|block|safety|moderat|finish|stop|type|kind|role|tool|name/i;
  const shape = (v, depth = 0) => {
    if (Array.isArray(v)) return v.length ? [shape(v[0], depth + 1), "len=" + v.length] : [];
    if (!v || typeof v !== "object") return typeof v;
    if (depth > 5) return "{…}";
    const o = {};
    for (const [k, x] of Object.entries(v)) {
      if (x && typeof x === "object") o[k] = shape(x, depth + 1);
      else if (STATE.test(k) && (typeof x !== "string" || x.length <= 60)) o[k] = x;
      else o[k] = typeof x;
    }
    return o;
  };
  const list = await rt.sendRequest("sessions.list", {});
  out.listKeys = Object.keys(list || {});
  const sessions = list?.sessions || [];
  out.sessionCount = sessions.length;
  const values = {};
  for (const s of sessions) for (const [k, v] of Object.entries(s)) if (STATE.test(k) && typeof v !== "object") (values[k] ||= {})[String(v)] = ((values[k] || {})[String(v)] || 0) + 1;
  out.sessionStateValues = values;
  out.sessionShapes = sessions.slice(0, 3).map((s) => shape(s));
  out.chats = [];
  for (const s of sessions.slice(0, 4)) {
    const h = await rt.sendRequest("chat.history", { session_id: s.session_id, limit: 200 });
    const kinds = {};
    for (const e of h.chat_events || []) {
      const key = [e.role, e.type, e.kind, e.event_type, e.payload?.type].filter(Boolean).join("/");
      (kinds[key] ||= { count: 0, shape: shape(e) }).count++;
    }
    out.chats.push({ status: s.status, historyKeys: Object.keys(h || {}), events: (h.chat_events || []).length, kinds });
  }
  const sources = performance.getEntriesByType("resource").map((e) => e.name).filter((u) => /\/static\/chunks\/[^?]+\.js/.test(u));
  const near = {};
  for (const u of sources) {
    const text = await fetch(u).then((r) => r.text()).catch(() => "");
    for (const word of ["sessions.list", "chat.history", "chat.cancel", "session_status", "sessionStatus", "activity", "Activity"]) {
      let at = text.indexOf(word);
      for (let n = 0; at >= 0 && n < 6; n++, at = text.indexOf(word, at + 1)) {
        for (const m of text.slice(Math.max(0, at - 600), at + 600).matchAll(/"([a-z_]{3,24})"/g)) (near[word] ||= {})[m[1]] = 1;
      }
    }
  }
  for (const k of Object.keys(near)) near[k] = Object.keys(near[k]).sort();
  out.wordsNearCalls = near;
  out.testIds = [...new Set([...document.querySelectorAll("[data-testid]")].map((e) => e.dataset.testid))].filter((t) => /activ|progress|step|tool|status|run|work|task/i.test(t)).sort();
  return out;
})()`

// liveMuseProbe runs museProbeScript on the first signed-in Muse account. It
// sends nothing and spends nothing.
func liveMuseProbe(t *testing.T, a *App) {
	recs, err := a.accounts.Records(accounts.ProviderMuse)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if !rec.Enabled || rec.Status != accounts.StatusSignedIn {
			continue
		}
		ctx, cancel := context.WithTimeout(a.ctx, 4*time.Minute)
		defer cancel()
		b, err := a.accounts.Browser(ctx, rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		owns := func(u *url.URL) bool { return strings.HasSuffix(u.Hostname(), "muse.ai") }
		var out any
		for i := 0; ; i++ {
			out, err = probeTab(ctx, b.Port, a, owns)
			if err == nil {
				if m, ok := out.(map[string]any); !ok || m["error"] == nil {
					break
				}
			}
			if i == 20 {
				t.Fatalf("probe: %v %s", err, dump(out))
			}
			time.Sleep(3 * time.Second)
		}
		t.Logf("muse probe on %s:\n%s", rec.Label(), dump(out))
		return
	}
	t.Fatal("no signed-in Muse account")
}

func probeTab(ctx context.Context, port int, a *App, owns func(*url.URL) bool) (any, error) {
	tab, _, err := browser.FindTab(ctx, port, a.cfg.Get().Accounts, owns)
	if err != nil {
		return nil, err
	}
	conn, err := browser.Dial(ctx, tab, port, 64<<20)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var out any
	err = conn.Evaluate(ctx, "(async()=>JSON.stringify(await "+museProbeScript+"))()", &out)
	return out, err
}
