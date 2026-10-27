# 0012. Production Hardening: Pay Once, Fail Closed, Durable Queues, Composable Nodes

- **Status:** Accepted
- **Date:** 2026-10-27
- **Amends:** 0005–0011 where noted below

## Context
A full review of the app before production use found the base features sound
but the error paths weak: several places could still pay twice for one
generation, the proxy could still fall back to the real IP, the Flow image
batch queue lived in React memory, journals and the account store did not
scale, platform limits were still constants or settings, and engine nodes could
not be chained across providers.

## Decision
1. **Pay once: an unknown outcome is re-checked, never re-sent.**
   - Flow: a listing or media read that fails is "unknown" (job stays pending);
     only a successful listing with no matching client ID may close a job as
     lost. A client ID found in the listing makes the job accepted. A finished
     video that is not downloaded keeps its job open.
   - Flow RPC errors are definite refusals only for codes that mean "not
     executed" (3, 7, 8, 9, 16); others (2, 4, 10, 13, 14, …) are reconciled.
   - Edit jobs are journaled only once a browser is ready, and use the same
     refusal rules as image and video.
   - Muse: an accepted send without a session ID, an unknown session status, a
     chat without the expected file and an unknown file size are re-checked.
     The time limit counts from `SentAt`; a resumed overdue job is read before
     anything is cancelled, and a failed cancel never leads to a resend. Bulk
     "retry failed" skips jobs Muse already accepted. Failures before a send do
     not spend attempts; re-checks back off; used-up hand-ons are capped per
     job and persisted.
   - Muse: a session ends only when `sessions.list` says `completed` or
     `interrupted` (checked live on muse.ai, 2026-10-27). The file is the
     chat's `delta.presentation` event (`payload.data.images|videos`); a
     follow-up question in the reply is not a failure. Completed without a
     file is a declined prompt ("CẦN ĐỔI PROMPT"), unless Muse's subscription,
     read at that moment, says the usage is gone (then the job moves to
     another account). Interrupted without a file retries in a new chat. The
     reply's wording is never classified.
   - Muse: a ratio and a video length are asked in the message line
     ("Tạo video · tỉ lệ 9:16 (dọc) · dài 8 giây"); the values offered are
     the ones measured live (`format.go`): images 16:9 (near 14:9), 4:3, 1:1,
     3:4, 9:16 (near 9:14), Flow's set; videos 9:16, 16:9, 1:1 and 2 to 10
     seconds (10 is Muse's longest). The first made file is measured and a job whose file
     differs gets a note; it is never sent again for that.
   - Queue tasks write their request ID before the request can leave the app.
   - A graph run with a `RequestID` derives every billable request ID (Flow) or
     job ID (Muse) from the run key, so running the same graph again resumes
     instead of paying again; only a definite refusal moves to a new attempt.
2. **Fail closed on proxies.** Only `proxy.None` is direct. An empty list fails
   for every other account; `accounts.ProxyURL` returns an error when no route
   is recorded; a proxy is marked dead only for proxy/tunnel navigation errors,
   not for Chrome launch failures; a signed-in account is never moved to
   another line on its own. An account with no route (`accounts.RouteError`,
   listed to the UI as `noRoute`) is not counted ready and takes no work:
   Flow leaves it out of rotation and the model list, Muse's scheduler skips
   it. Browsers are closed or removed only when idle
   (`CloseIfIdle`, `RemoveIfIdle`); idle headless browsers are reaped.
3. **Durable queues in Go on `internal/jobs`.** One generic journal (index by
   ID, atomic writes, transient progress, trim of finished records only,
   corrupt-file quarantine reported to the UI). The Flow image batch queue
   (Ảnh → Hàng loạt) moved from React into `flow/image_queue.go`; legacy
   All-mode generation journals are adopted and resumed. The Muse queue uses
   the same journal, and Muse's library of made files is kept apart from the
   queue so clearing finished jobs never empties it. The account store keeps
   one encrypted file per account and coalesces `accounts:updated`.
4. **Platform limits come from the platform.** Flow video ratios, resolutions
   and edit qualities are taken from the model catalogue; limits Flow does not
   expose in any reply the app reads are named fallbacks in
   `flow/limits.go`, logged on first use. Source size and trim limits are no
   longer settings. Image and video tokens use the site key discovered from
   the page, falling back to the built-in key with a warning.
5. **Composable nodes.** Ports carry `engine.MediaRef` values; node inputs and
   outputs are objects keyed by port name; `engine.Compatible` decides edges;
   pending provider results are `engine.PendingError`, never success;
   `OptionsFrom` sources are resolved by the providers; `Registry.RunGraph`
   runs a DAG. Providers are registered as plugins (`providers.Plugin`), so a
   third provider is a new package plus one line in `app.go`.
6. **One batch form.** Flow and Muse share `components/batch` and one prompt
   split rule (blank-line blocks when the text has blank lines, else one prompt
   per line).

## Consequences
- With no proxies saved, adding an account that is not set to "Không dùng
  proxy" fails with a message pointing to Trang chủ → Proxy.
- `config.json` gains `revision`, `accounts.maxDownloadBytes`,
  `accounts.idleBrowserMinutes` and the `flow.queue*` keys; Flow's
  `maxSourceBytes`, `minTrimSeconds`, `trimStepSeconds` and
  `maxDownloadBytes` are ignored when loaded (the download cap migrates).
- The Muse session states and the presentation event were read live
  (`SNAKEMEDIA_LIVE_APP=muse-probe` reads only; `muse-watch` sends prompts and
  logs every state); run them again when muse.ai changes.
- The Flow RPC discovery budget fields stay in `config.AccountConfig` because
  Flow's chrome layer, including the unedited `recaptcha.go`, takes that
  struct; they are no longer shown in the UI.
