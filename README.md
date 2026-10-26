# SnakeMedia

Desktop application built with Go + Wails v2 and a Vite + React + TypeScript frontend.

## Prerequisites
- Go (version in `go.mod`)
- Node.js 22+ and npm
- Wails CLI v2: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`

## Getting started
```bash
npm --prefix frontend install
wails dev
```
`wails dev` serves the app with hot reload and regenerates `frontend/wailsjs/`. A browser bridge with access to Go methods is also available at http://localhost:34115.

## Building
```bash
wails build
```
The executable is written to `build/bin/`.

## Name and version
Edit `wails.json` (`name`, `info.productName`, `info.productVersion`). Nothing else needs to change.

## Window size
The window opens at 70% of the primary screen's width and height (never below 800×600, never above the screen), centred. Change the fraction with `windowScale` (0.3–1.0) in `config.json`.

## Where data lives
Config and logs are stored under the OS user config directory, e.g. `%AppData%\SnakeMedia\config.json` and `%AppData%\SnakeMedia\logs\app.log` on Windows.

## Checks
All commands in `.agents/verify.json` must pass; CI runs the same set. See `AGENTS.md` for the full command table and conventions.

## Accounts and Google Flow sessions
The Account screen launches an isolated Chrome profile for each login. The backend owns the profile and browser endpoint, verifies email and Gaia ID before storing the account, and exposes metadata rather than cookies or tokens to React. The saved account card appears before any balance RPC; one separate refresh enriches it with credits and quota information. Enrichment failures retain the card and show a diagnostic rather than treating the user as signed out. A past balance is shown with its last verification time.

Account runtime policy lives in `config.json` under `accounts`: polling intervals, login/request/browser startup timeouts, refresh cooldown, response size, and script discovery limits. Defaults and validation are defined once in `internal/config/accounts.go`. `cdpPort` is an optional preferred port; zero lets the OS allocate an isolated port which is passed explicitly to Chrome. The launcher avoids Chrome's special `--remote-debugging-port=0` automation mode. An occupied unmanaged port is never adopted as an account session.

Build labels, CSRF/session tokens, language, User-Agent and cookies come from the active Flow page. The adapter reads compiled script sources from Chrome through CDP (including lazy bundles), discovers RPC IDs for the active build, caches discovery with bounded retries, and batches credits with optional availability queries. Discovery accepts metadata property order/quoting variations. Unsupported bundle/schema changes produce an enrichment error instead of falling back to captured values. Availability is retained as the server's raw code; missing optional availability metadata cannot hide a signed-in account or a valid balance and is not a guarantee of region, age, model or generation eligibility.

On Windows, `accounts.json` contains a versioned DPAPI-encrypted store bound to the current OS user. Other platforms use owner-only file permissions. Legacy plaintext stores migrate on read; old credentials and unverified balances are discarded because older refresh logic could associate them with the wrong account. Chrome profile directories are preserved. Invalid JSON is quarantined and account writes are blocked for that run so corrupt data cannot be silently overwritten.

Removing an account closes its managed Chrome session and permanently deletes its application credentials and profile directory (cookies, cache and local login data). Removal is restricted to directories under the managed profiles root; linked directories and profiles shared by other accounts are rejected. A deletion failure keeps the account listed so it can be retried. This does not delete the Google account itself. Closing an unfinished login cancels its work, closes the owned browser and removes the unsaved profile. Completed account browsers stay available until application shutdown. Automatic polling stops after success, cancellation or timeout; refresh work is serialized and respects the configured cooldown and server `Retry-After`.

Regression tests use synthetic sessions and local CDP/HTTP responses. To run the optional real Chrome lifecycle smoke test with a temporary profile and `about:blank`, set `SNAKEMEDIA_TEST_CHROME=1` and run `go test ./internal/services/chrome -run TestBrowserLifecycle -v`. This does not test an interactive Google login.


### Edit video runtime

Edit video requires Chrome and FFmpeg's `ffprobe` executable. The backend finds
`ffprobe` on `PATH`; an explicit executable can be selected with `flow.probePath`
in the user's `config.json`. No machine-specific executable path is shipped.
Video dimensions and duration come from the actual local file, independently of
whether WebView2 supports previewing its codec.

Uploaded sources are recorded under the user data directory's `flow/sources`.
The frontend receives an opaque source ID; ownership, project, workflow and media
IDs remain bound by the backend. Edits create a version in the uploaded workflow.
After upload, the backend waits for Flow's source processing to succeed and
publish a serving URL. The editor then uses Flow's normalized dimensions and
duration. Trim coordinates refer to that untrimmed source timeline.

`flow/jobs` journals each edit before submission. A timeout or lost response keeps
the existing job available through **Kiểm tra tiếp**, including after restart;
resuming never sends another generation request. Jobs do not contain cookies,
CSRF tokens or reCAPTCHA tokens. Runtime RPC descriptors and the reCAPTCHA key are
discovered from the active account's page, including its lazily loaded studio.

Upload retry/size policy, trim step/minimum and polling backoff live under `flow`
in `config.json`.
Script discovery has a separate `accounts.maxDiscoveryBytes` budget, so studio
bundles do not compete with the HTTP response limit. Downloads stream atomically
to disk rather than buffering entire videos in memory.

For the optional real integration test, set `SNAKEMEDIA_LIVE_EDIT=1`,
`SNAKEMEDIA_LIVE_DATA` to the existing application data directory and
`SNAKEMEDIA_LIVE_VIDEO` to a real source file and `SNAKEMEDIA_LIVE_PROMPT` to
the intended edit, then run
`go test ./internal/providers/flow -run '^TestLiveVideoEdit$' -v -count=1`.
`SNAKEMEDIA_LIVE_OUTPUT` optionally retains the result and job records in a chosen
folder. This test uploads a real file and can consume Flow credits; it does not
substitute simulated server results.

## Muse

The **Muse** tab queues image and video jobs on muse.ai and spreads them over
the Muse accounts saved under **Muse > Tài khoản Muse**. Each account has its
own Chrome profile under `%AppData%\SnakeMedia\muse\profiles`; add one by
signing in through Chrome or by pasting cookies exported from a signed-in
muse.ai page. Accounts and imported cookies are stored DPAPI-encrypted in
`muse/accounts.json`; the queue is journaled in `muse/jobs.json`.

A job that Muse already accepted is only checked again in its own chat, also
after a restart, so an interruption never spends a second generation. Timing,
retry and thread limits live under `muse` in `config.json`.

To open muse.ai anonymously in a real Chrome and check the page script, run
`SNAKEMEDIA_TEST_MUSE=1 go test ./internal/providers/muse -run TestLiveAnonymousPage -v`.
