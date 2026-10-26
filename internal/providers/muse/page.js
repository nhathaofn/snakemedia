// Runs INSIDE the muse.ai tab, never in Go. page.go wraps it as
// `(<this function>)(chunkSize)` and calls the returned window.__mss methods.
// It drives Muse's own React chat client (sendMessage, one new thread per job),
// reads sessions through Muse's gateway RPC (sessions.list / chat.history /
// chat.cancel) and downloads files through fs.raw in base64 chunks.
// Everything here depends on muse.ai internals: when Muse changes its client,
// fix it in this file.
function pageMain(CHUNK) {
  const rt = () => window.__hatchEarlyGatewayRuntimeState.activeRuntime.getSnapshot();
  // Muse's chat object: the React context value exposing sendMessage + ensureActiveChatSendReady.
  const chat = () => {
    if (!document.body) return null; // navigating: the next page is not built yet
    let root;
    for (const el of [document.body, ...document.querySelectorAll('body *')].slice(0, 80)) {
      const k = Object.keys(el).find((k) => k.startsWith('__reactContainer') || k.startsWith('__reactFiber'));
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
      if (v && typeof v.sendMessage === 'function' && typeof v.ensureActiveChatSendReady === 'function') return v;
      if (f.child) stack.push(f.child);
      if (f.sibling) stack.push(f.sibling);
    }
    return null;
  };
  const files = new Map();
  let fileSeq = 0;
  const b64 = (u8) => {
    let s = '';
    for (let i = 0; i < u8.length; i += 32768) s += String.fromCharCode.apply(null, u8.subarray(i, i + 32768));
    return btoa(s);
  };
  return (window.__mss = {
    async login() {
      try {
        const r = await fetch('/api/session', { credentials: 'include' });
        const j = await r.json().catch(() => ({}));
        return { loggedIn: r.status === 200 && Boolean(chat()), http: r.status, status: j.status || '', vm: j.vm_state || '' };
      } catch (e) {
        return { loggedIn: false, http: 0, status: '', vm: '' };
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
      if (!c) return { ok: false, sessionId: '', reason: `not_ready (${location.pathname})` };
      const r = await c.sendMessage(
        text,
        atts,
        thread
          ? { targetRoute: { kind: 'thread', sessionId: thread }, waitForReadiness: true }
          : { createNewThread: true, waitForReadiness: true },
      );
      return { ok: Boolean(r?.ok), sessionId: r?.sessionId || (r?.ok ? thread : ''), reason: String(r?.reason || r?.error || '') };
    },
    async status(sid) {
      const { sessions = [] } = await rt().sendRequest('sessions.list', {});
      return sessions.find((s) => s.session_id === sid)?.status || '';
    },
    async result(sid) {
      const h = await rt().sendRequest('chat.history', { session_id: sid, limit: 50 });
      const media = (list) =>
        (list || []).map((m) => ({ path: m.path || '', mime: m.mime || '', byteLen: Number(m.byte_len) || 0, missing: Boolean(m.missing) }));
      return (h.chat_events || [])
        .filter((e) => e.role === 'assistant')
        .map((e) => ({ text: e.display_text || '', images: media(e.payload?.data?.images), videos: media(e.payload?.data?.videos) }));
    },
    async open(path) {
      const r = await rt().sendRequest('fs.raw', { path: path.replace(/^sandbox:\/\//, '') });
      const blob = r instanceof Blob ? r : r.data;
      files.set(++fileSeq, blob);
      return { id: fileSeq, size: blob.size };
    },
    async slice(id, i) {
      return b64(new Uint8Array(await files.get(id).slice(i * CHUNK, (i + 1) * CHUNK).arrayBuffer()));
    },
    closeFile(id) {
      files.delete(id);
      return null;
    },
    async cancel(sid) {
      await rt().sendRequest('chat.cancel', { session_id: sid });
      return null;
    },
    // Quota: the Next.js server action "fetchSubscriptionAction". Its ID changes per Muse
    // build, so it is found in the loaded JS. Not found -> null (quota unknown).
    async quota() {
      if (!window.__mssQuotaId) {
        const re = /createServerReference\)\("([0-9a-f]{20,})",[^;]{0,120}?"fetchSubscriptionAction"\)/;
        const urls = [
          ...new Set(
            performance
              .getEntriesByType('resource')
              .map((e) => e.name)
              .filter((u) => /\.js(\?|$)/.test(u)),
          ),
        ];
        for (const u of urls) {
          const m = re.exec(await fetch(u).then((r) => r.text()).catch(() => ''));
          if (m) {
            window.__mssQuotaId = m[1];
            break;
          }
        }
        if (!window.__mssQuotaId) return null;
      }
      const r = await fetch('/', {
        method: 'POST',
        credentials: 'include',
        body: JSON.stringify([{}]),
        headers: { 'Next-Action': window.__mssQuotaId, Accept: 'text/x-component', 'Content-Type': 'text/plain;charset=UTF-8' },
      });
      if (!r.ok) {
        window.__mssQuotaId = '';
        return null;
      }
      for (const line of (await r.text()).split('\n')) {
        if (!line.includes('"success"')) continue;
        try {
          const j = JSON.parse(line.slice(line.indexOf(':') + 1));
          if (j.success && j.subscription) return j.subscription;
        } catch (e) {}
      }
      return null;
    },
  });
}
