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
  const ensureVisible = () => {
    try {
      const wasHidden =
        typeof document !== "undefined" && Boolean(document?.hidden);
      if (typeof Document !== "undefined" && Document?.prototype) {
        const hd = Object.getOwnPropertyDescriptor(
          Document.prototype,
          "hidden",
        );
        if (!hd || hd.configurable) {
          Object.defineProperty(Document.prototype, "hidden", {
            get: () => false,
            configurable: true,
          });
        }
        const vd = Object.getOwnPropertyDescriptor(
          Document.prototype,
          "visibilityState",
        );
        if (!vd || vd.configurable) {
          Object.defineProperty(Document.prototype, "visibilityState", {
            get: () => "visible",
            configurable: true,
          });
        }
      }
      if (
        typeof window !== "undefined" &&
        !window.__mssRafPatched &&
        typeof window.requestAnimationFrame === "function" &&
        typeof window.cancelAnimationFrame === "function"
      ) {
        window.__mssRafPatched = true;
        const raf = window.requestAnimationFrame.bind(window);
        const caf = window.cancelAnimationFrame.bind(window);
        const pending = new Map();
        let seq = 0;
        window.requestAnimationFrame = (cb) => {
          const id = ++seq;
          let done = false;
          let rId = 0;
          let timer = 0;
          const run = (ts) => {
            if (done) return;
            done = true;
            clearTimeout(timer);
            if (rId) caf(rId);
            pending.delete(id);
            cb(ts);
          };
          rId = raf(run);
          if (!done) {
            timer = setTimeout(() => run(performance.now()), 32);
            pending.set(id, [rId, timer]);
          }
          return id;
        };
        window.cancelAnimationFrame = (id) => {
          const v = pending.get(id);
          if (v) {
            caf(v[0]);
            clearTimeout(v[1]);
            pending.delete(id);
          }
        };
      }
      if (
        wasHidden &&
        typeof document?.dispatchEvent === "function" &&
        typeof Event === "function"
      ) {
        document.dispatchEvent(new Event("visibilitychange"));
        if (
          typeof window !== "undefined" &&
          typeof window.dispatchEvent === "function"
        ) {
          window.dispatchEvent(new Event("focus"));
        }
      }
    } catch (e) {}
  };
  ensureVisible();
  const gatewaySnapshot = () => {
    try {
      return (
        (typeof window !== "undefined" &&
          window.__hatchEarlyGatewayRuntimeState?.activeRuntime?.getSnapshot?.()) ||
        null
      );
    } catch (e) {
      return null;
    }
  };
  const gatewayDownKind = (snap) => {
    if (
      snap &&
      snap.isConnected === false &&
      (snap.connectionState === "error" ||
        snap.recoveryPaused ||
        snap.lastDialRefused)
    ) {
      return String(
        snap.lastFailureKind && snap.lastFailureKind !== "none"
          ? snap.lastFailureKind
          : snap.connectionState || "disconnected",
      );
    }
    return "";
  };
  const ensureGatewayReady = async (waitMs = 6000) => {
    ensureVisible();
    let snap = gatewaySnapshot();
    if (!snap || snap.isConnected !== false) return null;
    try {
      if (typeof snap.ensureLiveConnection === "function") {
        Promise.resolve(snap.ensureLiveConnection()).catch(() => {});
      } else if (typeof snap.reconnect === "function") {
        Promise.resolve(snap.reconnect()).catch(() => {});
      }
    } catch (e) {}
    const deadline = Date.now() + waitMs;
    let retried = false;
    while (Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, 250));
      snap = gatewaySnapshot();
      if (!snap || snap.isConnected !== false) return null;
      if (snap.recoveryPaused) ensureVisible();
      if (
        !retried &&
        Date.now() + waitMs / 2 >= deadline &&
        snap.connectionState === "error"
      ) {
        retried = true;
        try {
          if (typeof snap.reconnect === "function") {
            Promise.resolve(snap.reconnect()).catch(() => {});
          }
        } catch (e) {}
      }
    }
    return gatewayDownKind(gatewaySnapshot()) || null;
  };
  const fetchTimed = async (url, opts = {}, ms = 12000) => {
    const ac =
      typeof AbortController === "function" ? new AbortController() : null;
    const timer = ac && ms > 0 ? setTimeout(() => ac.abort(), ms) : null;
    try {
      return await fetch(url, {
        ...opts,
        ...(ac ? { signal: ac.signal } : {}),
      });
    } finally {
      if (timer) clearTimeout(timer);
    }
  };
  const rt = () =>
    window.__hatchEarlyGatewayRuntimeState.activeRuntime.getSnapshot();
  // Muse's chat object: the React context value exposing sendMessage + ensureActiveChatSendReady.
  // Reusing the matched Fiber cuts repeated 300k-node traversals across login/send calls,
  // while validating its pathname, DOM attachment, and live React root chain avoids staleness.
  const CHAT_CACHE_MS = 10000;
  let cachedChat = null;
  const reactRootProp = (el) =>
    Object.keys(el).find(
      (k) => k.startsWith("__reactContainer") || k.startsWith("__reactFiber"),
    );
  const findReactContainer = (body) => {
    if (body.children) {
      const q = [body];
      for (let i = 0; i < q.length && i < 80; i++) {
        const el = q[i];
        const k = reactRootProp(el);
        if (k) return { el, prop: k };
        const kids = el.children;
        if (kids) {
          for (let j = 0; j < kids.length && q.length < 80; j++) q.push(kids[j]);
        }
      }
      return null;
    }
    for (const el of [
      body,
      ...(typeof document.querySelectorAll === "function"
        ? document.querySelectorAll("body *")
        : []),
    ].slice(0, 80)) {
      const k = reactRootProp(el);
      if (k) return { el, prop: k };
    }
    return null;
  };
  const fiberMounted = (f, liveRoot) => {
    if (!f || !f.return) return false;
    let p = f;
    for (let i = 0; i < 1024 && p; i++) {
      if (
        p === liveRoot ||
        Boolean(liveRoot?.alternate && p === liveRoot.alternate)
      ) {
        return true;
      }
      p = p.return;
    }
    return false;
  };
  const TRPC_CLIENT_SYM =
    typeof Symbol === "function" ? Symbol.for("trpc_untypedClient") : null;
  const isCatchAllProxy = (v) => {
    try {
      if (TRPC_CLIENT_SYM && v[TRPC_CLIENT_SYM]) return true;
      return typeof v.__mss_invalid_prop__ !== "undefined";
    } catch (e) {
      return true;
    }
  };
  const validChatValue = (v) => {
    try {
      if (!v || typeof v !== "object") return null;
      if (isCatchAllProxy(v)) return null;
      if (
        typeof v.sendMessage !== "function" ||
        typeof v.ensureActiveChatSendReady !== "function"
      ) {
        return null;
      }
      if (!("sendMessage" in v) || !("ensureActiveChatSendReady" in v)) {
        return null;
      }
      return v;
    } catch (e) {
      return null;
    }
  };
  const cachedChatValue = (body, curPath) => {
    const entry = cachedChat;
    if (!entry) return null;
    if (
      entry.path !== curPath ||
      Date.now() - entry.at >= CHAT_CACHE_MS ||
      !(
        entry.el === document ||
        entry.el === document?.documentElement ||
        body === entry.el ||
        (typeof body.contains === "function" && body.contains(entry.el))
      )
    ) {
      cachedChat = null;
      return null;
    }
    const raw = entry.el?.[entry.prop];
    const liveRoot = raw?.stateNode?.current || raw;
    if (!liveRoot) {
      cachedChat = null;
      return null;
    }
    if (!entry.hadReturn) {
      const v = liveRoot === entry.fiber ? validChatValue(liveRoot.memoizedProps?.value) : null;
      if (!v) cachedChat = null;
      return v;
    }
    let live = null;
    if (fiberMounted(entry.fiber, liveRoot)) live = entry.fiber;
    else if (fiberMounted(entry.fiber?.alternate, liveRoot)) live = entry.fiber.alternate;
    const v = live ? validChatValue(live.memoizedProps?.value) : null;
    if (!v) {
      cachedChat = null;
      return null;
    }
    entry.fiber = live;
    return v;
  };
  const scanContainer = (found, curPath) => {
    if (!found) return null;
    let root = found.el[found.prop];
    if (root.stateNode?.current) root = root.stateNode.current;
    const stack = [root];
    for (let n = 0; stack.length && n < 300000; n++) {
      const f = stack.pop();
      const v = validChatValue(f.memoizedProps?.value);
      if (v) {
        cachedChat = {
          el: found.el,
          prop: found.prop,
          fiber: f,
          hadReturn: Boolean(f.return),
          path: curPath,
          at: Date.now(),
        };
        return v;
      }
      const ch = f.child;
      if (ch) stack.push(ch);
      const sb = f.sibling;
      if (sb) stack.push(sb);
    }
    return null;
  };
  const containerOn = (el) => {
    if (!el || typeof el !== "object") return null;
    const prop = reactRootProp(el);
    return prop ? { el, prop } : null;
  };
  const chat = () => {
    if (!document.body) return null; // navigating: the next page is not built yet
    const curPath =
      typeof location !== "undefined" ? String(location.pathname || "") : "";
    const cached = cachedChatValue(document.body, curPath);
    if (cached) return cached;
    const bodyFound = findReactContainer(document.body);
    const v = scanContainer(bodyFound, curPath);
    if (v) return v;
    for (const host of [document.documentElement, document]) {
      const hostFound = containerOn(host);
      if (hostFound && hostFound.el !== bodyFound?.el) {
        const hv = scanContainer(hostFound, curPath);
        if (hv) return hv;
      }
    }
    return null;
  };
  const chatDiag = () => {
    try {
      const seenRoots = new Set();
      const hints = [];
      for (const found of [
        document.body ? findReactContainer(document.body) : null,
        containerOn(document.documentElement),
        containerOn(document),
      ]) {
        if (!found) continue;
        let root = found.el[found.prop];
        if (root?.stateNode?.current) root = root.stateNode.current;
        if (!root || seenRoots.has(root)) continue;
        seenRoots.add(root);
        const stack = [root];
        for (let n = 0; stack.length && n < 50000; n++) {
          const f = stack.pop();
          const v = f?.memoizedProps?.value;
          if (v && typeof v === "object" && !isCatchAllProxy(v)) {
            if (
              typeof v.sendMessage === "function" ||
              typeof v.ensureActiveChatSendReady === "function"
            ) {
              hints.push(Object.keys(v).slice(0, 8).join(","));
              if (hints.length >= 3) break;
            }
          }
          if (f?.child) stack.push(f.child);
          if (f?.sibling) stack.push(f.sibling);
        }
      }
      return hints.length ? `ctx:[${hints.join("|")}]` : "";
    } catch (e) {
      return "";
    }
  };
  const waitRt = async (ms = 3000) => {
    const end = Date.now() + ms;
    for (;;) {
      try {
        const snap =
          window.__hatchEarlyGatewayRuntimeState?.activeRuntime?.getSnapshot?.();
        if (snap && typeof snap.sendRequest === "function") return snap;
      } catch (e) {}
      if (Date.now() >= end) break;
      await new Promise((r) => setTimeout(r, 150));
    }
    return rt();
  };
  const readyRt = async () => {
    ensureVisible();
    let snap = await waitRt();
    if (gatewayDownKind(snap)) {
      const gwErr = await ensureGatewayReady(5000);
      if (gwErr) {
        throw new Error("gateway disconnected: " + gwErr);
      }
      snap = await waitRt(1500);
    }
    if (gatewayDownKind(snap)) {
      throw new Error("gateway disconnected");
    }
    return snap;
  };
  const withTimeout = (promise, ms, label) => {
    if (!(ms > 0)) return promise;
    let timer = null;
    return Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(`${label} timeout`)), ms);
      }),
    ]).finally(() => {
      if (timer) clearTimeout(timer);
    });
  };
  const gwRequest = async (method, params, ms = 15000, label = method) => {
    const snap = await readyRt();
    return withTimeout(snap.sendRequest(method, params), ms, label);
  };
  // sessions.list lists every chat of the account, and every job opens one:
  // jobs polling the same tab side by side share one listing for a moment.
  // Shorter than the shortest poll interval the settings allow (1 s), so one
  // job never reads its own stale listing; a failed listing is not shared.
  const LIST_SHARE_MS = 800;
  let listed = null;
  const sessionList = () => {
    if (listed && Date.now() - listed.at < LIST_SHARE_MS) return listed.p;
    const p = gwRequest("sessions.list", {}, 8000, "sessions");
    const entry = { at: Date.now(), p };
    listed = entry;
    p.catch(() => {
      if (listed === entry) listed = null;
    });
    return p;
  };
  // Serialize composer send/route transitions on a shared tab so concurrent
  // threads (Send / SendTo) never interleave Next.js thread navigation or
  // read stale cachedChat context mid-transition.
  let sendTail = Promise.resolve();
  let routeBusy = 0;
  const routeWaiters = [];
  const notifyRouteIdle = () => {
    if (routeBusy === 0 && routeWaiters.length > 0) {
      const list = routeWaiters.splice(0, routeWaiters.length);
      for (const resolve of list) resolve();
    }
  };
  const withSendLock = (fn) => {
    const run = sendTail.then(fn, fn);
    sendTail = run.catch(() => {});
    return run;
  };
  const waitRouteSettled = async (ms = 2000) => {
    if (routeBusy === 0) return;
    await new Promise((resolve) => {
      let done = false;
      const finish = () => {
        if (done) return;
        done = true;
        resolve();
      };
      const timer = setTimeout(finish, ms);
      routeWaiters.push(() => {
        clearTimeout(timer);
        finish();
      });
    });
  };
  const FILE_TTL_MS = 10 * 60 * 1000;
  const MAX_OPEN_FILES = 16;
  const files = new Map();
  let fileSeq = 0;
  const sweepFiles = () => {
    const cutoff = Date.now() - FILE_TTL_MS;
    for (const [id, entry] of files) {
      if (!entry || entry.at < cutoff) files.delete(id);
    }
    while (files.size >= MAX_OPEN_FILES) {
      const oldest = files.keys().next().value;
      if (oldest === undefined) break;
      files.delete(oldest);
    }
  };
  const b64 = (u8) => {
    const parts = [];
    for (let i = 0; i < u8.length; i += 32768)
      parts.push(String.fromCharCode.apply(null, u8.subarray(i, i + 32768)));
    return btoa(parts.join(""));
  };
  const blobB64 = (blob) => {
    if (typeof FileReader === "function") {
      return new Promise((resolve, reject) => {
        const fr = new FileReader();
        fr.onload = () => {
          const s = String(fr.result || "");
          const at = s.indexOf(",");
          resolve(at >= 0 ? s.slice(at + 1) : "");
        };
        fr.onerror = () => reject(fr.error || new Error("FileReader failed"));
        fr.readAsDataURL(blob);
      });
    }
    return blob.arrayBuffer().then((buf) => b64(new Uint8Array(buf)));
  };

  // ---- Muse's own scripts: values the client keeps in its bundle ----
  const loadedChunks = () => [
    ...new Set(
      performance
        .getEntriesByType("resource")
        .map((e) => e.name)
        .filter((u) => /\/static\/(?:immutable\/)?chunks\/[^?]+\.js(\?|$)/.test(u)),
    ),
  ];
  const chunkTexts = new Map();
  let activeScans = 0;
  const source = (u) => {
    let pending = chunkTexts.get(u);
    if (!pending) {
      pending = fetchTimed(u, {}, 8000)
        .then((r) => {
          if (r.ok) return r.text();
          chunkTexts.delete(u);
          return "";
        })
        .catch(() => {
          chunkTexts.delete(u);
          return "";
        });
      chunkTexts.set(u, pending);
    }
    return pending;
  };
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
  // A scan that found nothing is not repeated for a while: every check would
  // otherwise fetch all of Muse's scripts again.
  const MISS_MS = 10 * 60 * 1000;
  const misses = new Map();
  // Scripts are fetched a few at a time; the first find in list order wins.
  const PARALLEL = 6;
  const first = async (urls, find, seen) => {
    for (let i = 0; i < urls.length; i += PARALLEL) {
      const texts = await Promise.all(urls.slice(i, i + PARALLEL).map(source));
      for (let j = 0; j < texts.length; j++) {
        const v = find(texts[j]);
        if (v != null) return v;
        if (seen) seen(urls[i + j], texts[j]);
      }
    }
    return null;
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
    if (Date.now() - (misses.get(memo) || -Infinity) < MISS_MS) return null;
    const loaded = loadedChunks();
    const remember = (v) => {
      try {
        localStorage.setItem(memo, JSON.stringify(v));
      } catch (e) {}
      return v;
    };
    const lazy = new Set();
    const known = new Set(loaded.map((u) => u.split("?")[0]));
    const listLazy = (u, text) => {
      const match = u.match(/\/static\/(?:immutable\/)?chunks\//);
      if (!match) return;
      const base = u.slice(0, match.index + 1);
      const query = u.includes("?") ? u.slice(u.indexOf("?")) : "";
      for (const m of text.matchAll(/static\/(?:immutable\/)?chunks\/[\w.-]+\.js/g)) {
        if (!known.has(base + m[0])) lazy.add(base + m[0] + query);
      }
    };
    activeScans++;
    try {
      const v =
        (await first(loaded, find, listLazy)) ?? (await first([...lazy], find));
      if (v != null) return remember(v);
      misses.set(memo, Date.now());
      return null;
    } finally {
      if (--activeScans <= 0) {
        activeScans = 0;
        chunkTexts.clear();
      }
    }
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
    const r = await fetchTimed(
      "/",
      {
        method: "POST",
        credentials: "include",
        body: JSON.stringify(args),
        headers: {
          "Next-Action": id,
          Accept: "text/x-component",
          "Content-Type": "text/plain;charset=UTF-8",
        },
      },
      12000,
    );
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
  // Muse's composer limits, the same for every chat: its DEFAULT_UPLOAD_CONFIG
  // object literal {maxFileSize, maxFilesPerUpload, supportedMimeTypes: [...]}.
  // Builds do not all keep the export name, so the literal itself is found.
  const uploadConfig = (text) => {
    const head = "{maxFileSize:";
    for (
      let from = text.indexOf(head);
      from >= 0;
      from = text.indexOf(head, from + 1)
    ) {
      let i = from;
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
        const v = JSON.parse(literal);
        if (v.maxFilesPerUpload > 0 && Array.isArray(v.supportedMimeTypes))
          return v;
      } catch (e) {}
    }
    return null;
  };

  return {
    async login(timeoutMs) {
      ensureVisible();
      const path =
        typeof location !== "undefined" ? String(location.pathname || "") : "";
      const ms = Number(timeoutMs) > 0 ? Number(timeoutMs) : 15000;
      try {
        const r = await fetchTimed(
          "/api/session",
          { credentials: "include" },
          ms,
        );
        const raw = await r.json().catch(() => null);
        const j = raw && typeof raw === "object" ? raw : {};
        if (r.status === 200 && routeBusy > 0 && !chat()) {
          await waitRouteSettled(Math.min(ms, 2000));
        }
        const c = Boolean(chat());
        const notice =
          !c && typeof document !== "undefined" && document.body
            ? String(document.body.innerText || "")
                .replace(/\s+/g, " ")
                .trim()
                .slice(0, 600)
            : "";
        return {
          loggedIn: r.status === 200 && c,
          chat: c,
          http: r.status,
          status: String(j.status || ""),
          vm: String(j.vm_state || ""),
          gateway: gatewayDownKind(gatewaySnapshot()),
          error: String(
            j.error ||
              j.reason ||
              j.message ||
              (j.banned ? "banned" : "") ||
              (j.suspended ? "suspended" : "") ||
              (j.disabled ? "disabled" : ""),
          ),
          path,
          notice,
        };
      } catch (e) {
        return {
          loggedIn: false,
          chat: false,
          http: 0,
          status: "",
          vm: "",
          error: String(e?.message || ""),
          path,
          notice: "",
        };
      }
    },
    // thread = an existing chat's sessionId: send into that chat; empty = a new thread.
    async send(text, atts, thread) {
      return withSendLock(async () => {
        ensureVisible();
        // The page may be rebuilding (the user opened another chat): wait up to 20s for the client.
        let c = chat();
        for (let k = 0; !c && k < 20; k++) {
          await new Promise((r) => setTimeout(r, 1000));
          c = chat();
        }
        if (!c) {
          const diag = chatDiag();
          const path =
            typeof location !== "undefined" ? String(location.pathname || "") : "";
          return {
            ok: false,
            sessionId: "",
            reason: diag ? `not_ready (${path}; ${diag})` : `not_ready (${path})`,
          };
        }
        const gwErr = await ensureGatewayReady();
        if (gwErr) {
          cachedChat = null;
          return {
            ok: false,
            sessionId: "",
            reason: `not_ready (gateway:${gwErr})`,
          };
        }
        const beforePath =
          typeof location !== "undefined" ? String(location.pathname || "") : "";
        let r;
        routeBusy++;
        try {
          r = await c.sendMessage(
            text,
            atts,
            thread
              ? {
                  targetRoute: { kind: "thread", sessionId: thread },
                  waitForReadiness: true,
                }
              : { createNewThread: true, waitForReadiness: true },
          );
        } catch (err) {
          cachedChat = null;
          throw err;
        } finally {
          routeBusy--;
          notifyRouteIdle();
        }
        const afterPath =
          typeof location !== "undefined" ? String(location.pathname || "") : "";
        if (
          thread ||
          afterPath !== beforePath ||
          (!r?.ok && String(r?.reason || "").startsWith("not_ready"))
        ) {
          cachedChat = null;
        }
        return {
          ok: Boolean(r?.ok),
          sessionId: r?.sessionId || (r?.ok ? thread : ""),
          reason: String(r?.reason || r?.error || ""),
          // Muse returns both reason: "send_failed" and an Error. Keep the
          // latter's useful fields: choosing reason || error hid the cause.
          diagnostics: r?.error
            ? Object.fromEntries(
                ["name", "message", "code", "kind", "deliveryState", "requestTimedOut"]
                  .filter((k) => ["string", "number", "boolean"].includes(typeof r.error[k]))
                  .map((k) => {
                    let v = r.error[k];
                    if (typeof v === "string") {
                      if (text) v = v.split(text).join("[prompt]");
                      v = v.replace(/[A-Za-z0-9+/=]{128,}/g, "[data]").slice(0, 1024);
                    }
                    return [k, v];
                  }),
              )
            : null,
        };
      });
    },
    async status(sid) {
      const { sessions = [] } = await sessionList();
      return sessions.find((s) => s.session_id === sid)?.status || "";
    },
    async sessions(fresh) {
      const { sessions = [] } = fresh
        ? await gwRequest("sessions.list", {}, 8000, "sessions")
        : await sessionList();
      return sessions.map((s) => ({
        id: s.session_id || "",
        status: s.status || "",
      }));
    },
    async userMessages(sid) {
      const h = await gwRequest(
        "chat.history",
        {
          session_id: sid,
          limit: 500,
        },
        20000,
      );
      const textOf = (e) => {
        for (const v of [
          e?.display_text,
          e?.text,
          e?.content,
          e?.payload?.display_text,
          e?.payload?.text,
          e?.payload?.content,
          e?.payload?.message,
          e?.payload?.data?.text,
          e?.payload?.data?.content,
        ]) {
          if (typeof v === "string" && v.trim()) return v;
        }
        if (Array.isArray(e?.content)) {
          return e.content
            .map((p) => (typeof p?.text === "string" ? p.text : ""))
            .filter(Boolean)
            .join("\n");
        }
        return "";
      };
      return (h.chat_events || [])
        .filter((e) => e.role === "user")
        .map((e) => {
          const text = textOf(e);
          return { text, hasText: Boolean(text) };
        });
    },
    // The chat's raw events, tool calls included (TestLiveConsole only).
    async history(sid) {
      return await gwRequest(
        "chat.history",
        {
          session_id: sid,
          limit: 500,
        },
        20000,
      );
    },
    async result(sid) {
      const h = await gwRequest(
        "chat.history",
        {
          session_id: sid,
          limit: 500,
        },
        20000,
      );
      const mediaKey = (p) =>
        String(p || "")
          .trim()
          .replace(/^sandbox:\/\//, "")
          .replace(/^\/+/, "");
      const inferMime = (path, kind) => {
        const clean = String(path || "")
          .split(/[?#]/)[0]
          .toLowerCase();
        if (clean.endsWith(".mp4") || clean.endsWith(".m4v")) return "video/mp4";
        if (clean.endsWith(".webm")) return "video/webm";
        if (clean.endsWith(".mov")) return "video/quicktime";
        if (clean.endsWith(".webp")) return "image/webp";
        if (clean.endsWith(".png")) return "image/png";
        if (clean.endsWith(".jpg") || clean.endsWith(".jpeg")) return "image/jpeg";
        if (clean.endsWith(".gif")) return "image/gif";
        const k = String(kind || "").toLowerCase();
        if (k === "video") return "video/mp4";
        if (k === "image") return "image/webp";
        return "";
      };
      const parseMeta = (raw) => {
        if (!raw) return null;
        if (typeof raw === "object") return raw;
        if (typeof raw === "string") {
          try {
            const parsed = JSON.parse(raw);
            return parsed && typeof parsed === "object" ? parsed : null;
          } catch (e) {}
        }
        return null;
      };
      const normalizePath = (p) => {
        const s = String(p || "").trim();
        if (!s) return "";
        if (s.startsWith("sandbox://") || s.startsWith("/")) return s;
        return "sandbox://" + s;
      };
      const isSandboxOrLocal = (p) => Boolean(p) && !/^https?:\/\//i.test(p);
      const addItem = (list, seen, item) => {
        if (!item.path || !item.mime) return;
        const key = mediaKey(item.path);
        if (!key) return;
        const idx = seen.get(key);
        if (idx !== undefined) {
          const prev = list[idx];
          if (prev.missing && !item.missing) {
            list[idx] = item;
          } else if (prev.byteLen <= 0 && item.byteLen > 0) {
            list[idx] = { ...prev, byteLen: item.byteLen };
          }
          return;
        }
        seen.set(key, list.length);
        list.push(item);
      };
      const extractMedia = (e) => {
        const images = [];
        const videos = [];
        const seenImages = new Map();
        const seenVideos = new Map();
        const pushTyped = (item, kindHint = "") => {
          const path = String(item.path || "").trim();
          const mime = String(item.mime || "").trim() || inferMime(path, kindHint);
          if (!path || !mime) return;
          const entry = {
            path,
            mime,
            byteLen: Number(item.byteLen) || 0,
            missing: Boolean(item.missing),
          };
          if (mime.startsWith("video/")) {
            addItem(videos, seenVideos, entry);
          } else if (mime.startsWith("image/")) {
            addItem(images, seenImages, entry);
          }
        };
        for (const m of e.payload?.data?.images || []) {
          pushTyped(
            {
              path: m.path || "",
              mime: m.mime || "",
              byteLen: m.byte_len,
              missing: m.missing,
            },
            "image",
          );
        }
        for (const m of e.payload?.data?.videos || []) {
          pushTyped(
            {
              path: m.path || "",
              mime: m.mime || "",
              byteLen: m.byte_len,
              missing: m.missing,
            },
            "video",
          );
        }
        // Muse often delivers generated files directly on message.assistant
        // in e.resources (or e.payload.resources) without a separate
        // delta.presentation event.
        for (const list of [e.resources, e.payload?.resources]) {
          if (!Array.isArray(list)) continue;
          for (const r of list) {
            if (!r || typeof r !== "object") continue;
            const meta = parseMeta(r.metadata);
            const path =
              [r.uri, r.path, normalizePath(meta?.path)]
                .map((v) => String(v || "").trim())
                .find(isSandboxOrLocal) || "";
            if (!path) continue;
            const kind = r.kind || meta?.kind || "";
            const mime =
              String(
                r.mime_type || r.mime || meta?.mime_type || meta?.mime || "",
              ).trim() || inferMime(path, kind);
            const byteLen =
              Number(
                r.byte_len ?? r.size_bytes ?? meta?.size_bytes ?? meta?.byte_len,
              ) || 0;
            const missing = Boolean(r.missing || meta?.missing);
            pushTyped({ path, mime, byteLen, missing }, kind);
          }
        }
        // Fallback: markdown media embeds ![...](sandbox://...) in assistant
        // text or fallback_text when neither presentation nor resources carried them.
        for (const rawText of [
          e.display_text,
          e.payload?.data?.fallback_text,
          e.payload?.fallback_text,
        ]) {
          if (typeof rawText !== "string" || !rawText.includes("sandbox://"))
            continue;
          for (const match of rawText.matchAll(
            /!\[[^\]]*\]\((sandbox:\/\/[^)\s]+)\)/g,
          )) {
            const path = match[1];
            if (/(?:^|\/)(?:uploads|attachments)\//i.test(mediaKey(path)))
              continue;
            const mime = inferMime(path, "");
            if (!mime) continue;
            pushTyped({ path, mime, byteLen: 0, missing: false });
          }
        }
        return { images, videos };
      };
      return (h.chat_events || [])
        .filter((e) => e.role === "assistant")
        .map((e) => {
          const { images, videos } = extractMedia(e);
          return {
            text: e.display_text || "",
            images,
            videos,
          };
        });
    },
    async open(path, maxBytes) {
      sweepFiles();
      const r = await gwRequest(
        "fs.raw",
        {
          path: path.replace(/^sandbox:\/\//, ""),
        },
        90000,
      );
      const blob = r instanceof Blob ? r : r.data;
      if (!blob || blob.size <= 0 || blob.size > maxBytes)
        throw new Error("Muse file is empty or exceeds the download limit");
      if (blob.size <= CHUNK) {
        return { id: 0, size: blob.size, data: await blobB64(blob) };
      }
      const id = ++fileSeq;
      files.set(id, { blob, at: Date.now() });
      return { id, size: blob.size };
    },
    async slice(id, i) {
      const entry = files.get(id);
      if (!entry?.blob) throw new Error("Muse file handle expired or closed");
      entry.at = Date.now();
      return blobB64(entry.blob.slice(i * CHUNK, (i + 1) * CHUNK));
    },
    closeFile(id) {
      files.delete(id);
      return null;
    },
    async cancel(sid) {
      await gwRequest("chat.cancel", { session_id: sid }, 10000);
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
        models = await gwRequest("model.get", {}, 10000);
      } catch (e) {}
      return { upload, models };
    },
  };
}
