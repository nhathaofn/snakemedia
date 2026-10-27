# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is the primary project guide (stack, full command table, repo map, conventions, boundaries, decision index). It is imported here so there is one source to maintain:

@AGENTS.md

The notes below add what AGENTS.md does not cover.

## Running a subset of tests
- Go, one package / one test: `go test ./internal/providers/flow -run '^TestName$' -v -count=1`
- Frontend, one file or name: `npm --prefix frontend run test -- src/features/muse/theme.test.ts` or `... -- -t "name"`
- Frontend type check only: `npm --prefix frontend run typecheck`
- `frontend/dist` must exist before any Go command (`main.go` embeds it); run `npm --prefix frontend run build` once if `go test`/`go build` fails on the embed.
- Opt-in live tests (real Chrome / real accounts, skipped by default):
  - `SNAKEMEDIA_LIVE_EDIT=1`, `SNAKEMEDIA_LIVE_DATA`, `SNAKEMEDIA_LIVE_VIDEO`, `SNAKEMEDIA_LIVE_PROMPT` → `go test ./internal/providers/flow -run '^TestLiveVideoEdit$' -v -count=1`
  - `SNAKEMEDIA_TEST_MUSE=1` → `go test ./internal/providers/muse -v -count=1` (opens muse.ai in a real Chrome)
  - `SNAKEMEDIA_LIVE_APP=list,credits,flow-image,flow-video,muse-image,muse-video` → `go test . -run '^TestLiveApp$' -v -count=1` (whole app on the real data folder; paid steps spend credits/quota; `SNAKEMEDIA_LIVE_DIRECT=<ids>` sets accounts to no proxy first)
  - `SNAKEMEDIA_TEST_CHROME=1` → `go test ./internal/browser -run TestBrowserLifecycle -v -count=1` (local Chrome smoke test)
- On machines with Windows Application Control, binaries built into `%TEMP%` may be blocked (go test, `wails generate module`). Retry, or point `TEMP`/`TMP` at a folder under `build/bin/` for the command.

## Architecture: how a request travels
1. React feature → `frontend/src/services/api.ts` (typed wrapper over generated `wailsjs/go/main/App`, unwraps `models.Response[T]`).
2. `app.go` method → thin delegation, wrapped in `logged(...)` so failures land in the log file. Long-running calls register with `services.RequestRegistry` under the frontend's request ID so `CancelGeneration` can stop the app-side wait (the job on the provider side keeps running and stays resumable).
3. Provider (`internal/providers/flow`, `internal/providers/muse`) owns its protocol, caches and on-disk job journals; generated/imported files go through the shared `media.Store`, served to the WebView via the `/flow-media/` middleware.
4. Progress flows back via typed events (`internal/events`) consumed with `useWailsEvent` (`accounts:updated` feeds `useAccountsStore` through `useAccountsLive`).

`NewApp` in `app.go` is the composition root: paths → config → logger → `providers.Prepare(cfg, flow.Plugin(), muse.Plugin())` → `accounts.Service` (with every plugin's `Site`) → media store → `Build` the providers → `Register` their nodes and option sources plus `media.save` into the `engine.Registry`. A third provider is a new package with a `Plugin()` and one more argument there. Startup/shutdown go through the plugin set (`Start`/`Close`, in plugin order); shutdown cancels in-flight requests first.

## Accounts, browsers and proxies (ADR 0010)
- `internal/accounts` is the only account store, for every provider. A provider contributes an `accounts.Site` (home URL, owned URLs, cookie domains, `Inspect` that reads identity/usage from the page) and keeps its own data in `Record.Data` (Flow: `session/site.go` `flowData`; Muse: `muse/site.go` `museData`). Sign-in is a watched visible window or imported cookies; the frontend never polls a login.
- Providers get a browser with `accounts.Service.Browser(ctx, id)` (headless unless a window is open, on the account's proxy, site tab ensured) and hold it with `Acquire` while work runs, which blocks restarts, proxy changes and removal.
- `internal/browser` is the only Chrome/CDP code. `flow/chrome/browser.go` aliases the shared helpers to the short names `recaptcha.go` (never edited) uses.
- `internal/proxy.Manager.Route` picks the proxy; Go-side HTTP for an account must use `ProxyURL` (Flow: `FlowClient.SetProxy` in `flow/client.go`).
- Flow's provider sees accounts through `session.Service` (`flow.Accounts` interface); tests use the in-memory fake in `flow/fakes_test.go`. Muse tests seed records with `accounts.Service.Seed`.

## Providers
- **Flow** (Google Flow/Labs): image, video and video-edit generation over account rotation, plus the durable image batch queue (`image_queue.go`, Ảnh → Hàng loạt) that resumes by request ID after a restart; see ADRs 0005–0007 and 0012.
- **Muse** (muse.ai): drives Muse's own chat client inside a signed-in muse.ai tab via the embedded `page.js` (evaluated over CDP by `page.go`). Every muse.ai page dependency lives in `page.js`. Owns a durable image/video job queue and scheduler. What Muse allows (quota, upload limits, models) is read from muse.ai per account, never configured. See ADRs 0008 and 0011.
- Both providers follow "never pay twice": a job that may already have been sent is only re-checked (resumed), never re-submitted. Preserve this when touching job/retry code. ADR 0012 lists the reconciliation rules (what counts as a definite refusal, timeouts from `SentAt`, bulk retry skipping accepted jobs, graph run keys).

## Frontend sections
Header sections are `home` (overview, every provider's accounts, proxy list), `flow`, `muse` and `settings` under `frontend/src/features/`, each with a `section.tsx` manifest registered in `frontend/src/routes/registry.ts`. Settings screens use `frontend/src/components/settings` and `useConfigSection`.
