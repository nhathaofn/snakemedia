// Runs INSIDE the muse.ai tab, never in Go. page.go evaluates it as
// `(<this function>)(chunkSize)` once per page load (under a key that changes
// with this file) and calls the returned methods.
// It drives Muse's own React chat client (sendMessage, one new thread per job),
// reads sessions and the account's model through Muse's gateway RPC
// (sessions.list / chat.history / chat.cancel / model.get), downloads files
// through fs.raw in base64 chunks, and reads the subscription and upload
// limits the way Muse's own client does.
// Everything here depends on muse.ai internals: when Muse changes its client,
// fix it in this file.
function pageMain(CHUNK) {
  const rt = () =>
    window.__hatchEarlyGatewayRuntimeState.activeRuntime.getSnapshot();
  // Muse's chat object: the React context value exposing sendMessage + ensureActiveChatSendReady.
  const chat = () => {
    if (!document.body) return null; // navigating: the next page is not built yet
    let root;
    for (const el of [
      document.body,
      ...document.querySelectorAll("body *"),
    ].slice(0, 80)) {
      const k = Object.keys(el).find(
        (k) => k.startsWith("__reactContainer") || k.startsWith("__reactFiber"),
      );
      if (k) {
        root = el[k];
        break;
      }
    }
    if (!root) return null;
    if (root.stateNode?.current) root = root.stateNode.current;
    const stack = [root];
    for (let n = 0; stack.length && n < 300000; n++) {
      const f = stack.pop();
      const v = f.memoizedProps?.value;
      if (
        v &&
        typeof v.sendMessage === "function" &&
        typeof v.ensureActiveChatSendReady === "function"
      )
        return v;
      if (f.child) stack.push(f.child);
      if (f.sibling) stack.push(f.sibling);
    }
    return null;
  };
  const files = new Map();
  let fileSeq = 0;
  const b64 = (u8) => {
    let s = "";
    for (let i = 0; i < u8.length; i += 32768)
      s += String.fromCharCode.apply(null, u8.subarray(i, i + 32768));
    return btoa(s);
  };

  // ---- Muse's own scripts: values the client keeps in its bundle ----
  const loadedChunks = () => [
    ...new Set(
      performance
        .getEntriesByType("resource")
        .map((e) => e.name)
        .filter((u) => /\/static\/chunks\/[^?]+\.js(\?|$)/.test(u)),
    ),
  ];
  const source = (u) =>
    fetch(u)
      .then((r) => (r.ok ? r.text() : ""))
      .catch(() => "");
  // The build the page runs: a found value is remembered per build, because
  // Muse's IDs change with every deployment. Next.js names its build when the
  // page carries __NEXT_DATA__; otherwise the set of loaded chunk files (their
  // names hash their content) stands for it, whatever order they loaded in.
  const build = () => {
    const id = window.__NEXT_DATA__?.buildId;
    if (id) return String(id);
    const names = loadedChunks()
      .map((u) => u.split("?")[0])
      .sort()
      .join("|");
    let h = 0x811c9dc5;
    for (let i = 0; i < names.length; i++)
      h = Math.imul(h ^ names.charCodeAt(i), 0x01000193);
    return (h >>> 0).toString(16);
  };
  // Values remembered for older builds are never read again: drop them once.
  let swept = false;
  const sweep = (current) => {
    if (swept) return;
    swept = true;
    try {
      for (let i = localStorage.length - 1; i >= 0; i--) {
        const k = localStorage.key(i);
        if (k && k.startsWith("__mss:") && !k.endsWith(`:${current}`))
          localStorage.removeItem(k);
      }
    } catch (e) {}
  };
  // scan returns the first non-null find(source) over the loaded scripts, then
  // over the lazy chunks they list ("static/chunks/x.js"), which the page has
  // not needed yet (the subscription screen, for one).
  const scan = async (key, find) => {
    const b = build();
    sweep(b);
    const memo = `__mss:${key}:${b}`;
    try {
      const kept = localStorage.getItem(memo);
      if (kept) return JSON.parse(kept);
    } catch (e) {}
    const loaded = loadedChunks();
    const remember = (v) => {
      try {
        localStorage.setItem(memo, JSON.stringify(v));
      } catch (e) {}
      return v;
    };
    const lazy = new Set();
    const known = new Set(loaded.map((u) => u.split("?")[0]));
    for (const u of loaded) {
      const text = await source(u);
      const v = find(text);
      if (v != null) return remember(v);
      const base = u.slice(0, u.indexOf("/static/chunks/") + 1);
      const query = u.includes("?") ? u.slice(u.indexOf("?")) : "";
      for (const m of text.matchAll(/static\/chunks\/[\w.-]+\.js/g)) {
        if (!known.has(base + m[0])) lazy.add(base + m[0] + query);
      }
    }
    for (const u of lazy) {
      const v = find(await source(u));
      if (v != null) return remember(v);
    }
    return null;
  };
  // A Next.js server action is registered as createServerReference)("<id>",…,"<name>").
  const actionId = (name) =>
    scan(`action:${name}`, (text) => {
      const m = new RegExp(
        `createServerReference\\)\\("([0-9a-f]{20,})",[^;]{0,200}?"${name}"\\)`,
      ).exec(text);
      return m ? m[1] : null;
    });
  const callAction = async (name, args) => {
    const id = await actionId(name);
    if (!id) return null;
    const r = await fetch("/", {
      method: "POST",
      credentials: "include",
      body: JSON.stringify(args),
      headers: {
        "Next-Action": id,
        Accept: "text/x-component",
        "Content-Type": "text/plain;charset=UTF-8",
      },
    });
    if (!r.ok) {
      try {
        localStorage.removeItem(`__mss:action:${name}:${build()}`);
      } catch (e) {}
      return null;
    }
    // The reply is "<row>:<json>" lines; row 1 holds the action's result.
    for (const line of (await r.text()).split("\n")) {
      const at = line.indexOf(":");
      if (line.slice(0, at) !== "1") continue;
      try {
        return JSON.parse(line.slice(at + 1));
      } catch (e) {}
    }
    return null;
  };
  // Muse's composer limits: the object exported as DEFAULT_UPLOAD_CONFIG
  // ({maxFileSize, maxFilesPerUpload, supportedMimeTypes: [...]}).
  const uploadConfig = (text) => {
    const at = text.indexOf('"DEFAULT_UPLOAD_CONFIG",0,');
    if (at < 0) return null;
    const name = /^"DEFAULT_UPLOAD_CONFIG",0,([\w$]+)/.exec(
      text.slice(at),
    )?.[1];
    const start = name ? text.lastIndexOf(`${name}={maxFileSize:`, at) : -1;
    if (start < 0) return null;
    let i = start + name.length + 1;
    const from = i;
    for (let depth = 0; i < text.length; i++) {
      if (text[i] === "{") depth++;
      else if (text[i] === "}" && --depth === 0) break;
    }
    const literal = text
      .slice(from, i + 1)
      .replace(/([{,])([A-Za-z_$][\w$]*):/g, '$1"$2":')
      .replace(/\b0x[0-9a-f]+\b/gi, (h) => String(parseInt(h, 16)))
      .replace(/!0\b/g, "true")
      .replace(/!1\b/g, "false");
    try {
      return JSON.parse(literal);
    } catch (e) {
      return null;
    }
  };

  return {
    async login() {
      try {
        const r = await fetch("/api/session", { credentials: "include" });
        const j = await r.json().catch(() => ({}));
        const c = Boolean(chat());
        return {
          loggedIn: r.status === 200 && c,
          chat: c,
          http: r.status,
          status: j.status || "",
          vm: j.vm_state || "",
        };
      } catch (e) {
        return { loggedIn: false, chat: false, http: 0, status: "", vm: "" };
      }
    },
    // thread = an existing chat's sessionId: send into that chat; empty = a new thread.
    async send(text, atts, thread) {
      // The page may be rebuilding (the user opened another chat): wait up to 20s for the client.
      let c = chat();
      for (let k = 0; !c && k < 20; k++) {
        await new Promise((r) => setTimeout(r, 1000));
        c = chat();
      }
      if (!c)
        return {
          ok: false,
          sessionId: "",
          reason: `not_ready (${location.pathname})`,
        };
      const r = await c.sendMessage(
        text,
        atts,
        thread
          ? {
              targetRoute: { kind: "thread", sessionId: thread },
              waitForReadiness: true,
            }
          : { createNewThread: true, waitForReadiness: true },
      );
      return {
        ok: Boolean(r?.ok),
        sessionId: r?.sessionId || (r?.ok ? thread : ""),
        reason: String(r?.reason || r?.error || ""),
      };
    },
    async status(sid) {
      const { sessions = [] } = await rt().sendRequest("sessions.list", {});
      return sessions.find((s) => s.session_id === sid)?.status || "";
    },
    async result(sid) {
      const h = await rt().sendRequest("chat.history", {
        session_id: sid,
        limit: 50,
      });
      const media = (list) =>
        (list || []).map((m) => ({
          path: m.path || "",
          mime: m.mime || "",
          byteLen: Number(m.byte_len) || 0,
          missing: Boolean(m.missing),
        }));
      // Small status-like fields of an event (its status, error code, refusal
      // or safety flags), so Go can tell why no file came without reading the
      // reply's wording. Message text and file lists are never collected.
      const SIGNAL =
        /status|state|error|code|reason|refus|block|safety|moderat/i;
      const SKIP = new Set([
        "display_text",
        "text",
        "content",
        "images",
        "videos",
      ]);
      const signals = (event) => {
        const out = {};
        let n = 0;
        const walk = (v, path, depth) => {
          if (!v || typeof v !== "object" || depth > 4) return;
          for (const [k, x] of Object.entries(v)) {
            if (n >= 16) return;
            if (SKIP.has(k)) continue;
            const key = path ? `${path}.${k}` : k;
            if (x && typeof x === "object") walk(x, key, depth + 1);
            else if (SIGNAL.test(k) && x !== undefined) {
              const s = String(x);
              if (s.length <= 80) {
                out[key] = s;
                n++;
              }
            }
          }
        };
        walk(event, "", 0);
        return out;
      };
      return (h.chat_events || [])
        .filter((e) => e.role === "assistant")
        .map((e) => ({
          text: e.display_text || "",
          images: media(e.payload?.data?.images),
          videos: media(e.payload?.data?.videos),
          signals: signals(e),
        }));
    },
    async open(path) {
      const r = await rt().sendRequest("fs.raw", {
        path: path.replace(/^sandbox:\/\//, ""),
      });
      const blob = r instanceof Blob ? r : r.data;
      files.set(++fileSeq, blob);
      return { id: fileSeq, size: blob.size };
    },
    async slice(id, i) {
      return b64(
        new Uint8Array(
          await files
            .get(id)
            .slice(i * CHUNK, (i + 1) * CHUNK)
            .arrayBuffer(),
        ),
      );
    },
    closeFile(id) {
      files.delete(id);
      return null;
    },
    async cancel(sid) {
      await rt().sendRequest("chat.cancel", { session_id: sid });
      return null;
    },
    // The subscription Muse's settings screen shows (fetchSubscriptionAction):
    // tier, weekly usage and additional tokens. null when it cannot be read.
    async quota() {
      const r = await callAction("fetchSubscriptionAction", [{}]);
      return r && r.success && r.subscription ? r.subscription : null;
    },
    // What this account may send and use: Muse's upload limits and its models.
    async capabilities() {
      const upload = await scan("upload", uploadConfig);
      let models = null;
      try {
        models = await rt().sendRequest("model.get", {});
      } catch (e) {}
      return { upload, models };
    },
  };
}
