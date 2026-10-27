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

## Accounts and proxies

**Trang chủ > Tài khoản** lists the accounts of every provider (a tab each for
Google Flow and Muse); **Trang chủ > Proxy** holds the proxy list they share.
Each account has its own Chrome profile under `%AppData%\SnakeMedia\accounts\profiles`.
Add one by signing in through the Chrome window the app opens (it notices the
session by itself) or by pasting cookies exported from a signed-in page; cookies
for other sites are dropped. Accounts, imported cookies and the proxy list are
stored DPAPI-encrypted under `accounts/` (owner-only files on other platforms);
a corrupt store is moved aside and blocks writes for that run. React only sees
account metadata, never cookies or session tokens.

Every account browser runs on its proxy: an explicit line, "none", or the least
shared live line of the list, kept for that account. Chrome and the app's own
requests for the account go through a local bridge on `127.0.0.1`; when the
proxy fails the account stops instead of using the machine's real IP. WebRTC is
limited to the proxy as well. Only "Không dùng proxy" goes direct: with an empty
proxy list every other account refuses to start, and a signed-in account whose
proxy stops answering waits for it (or for the user to pick another line)
rather than moving to a different IP. Idle headless account browsers close after
`accounts.idleBrowserMinutes`.

Removing an account closes its Chrome and deletes its profile directory
(cookies, cache, login data); it does not delete the account on the site. Shared
browser policy (headless, timeouts, polling) lives under `accounts` in
`config.json` and on **Cài đặt > Chung**; defaults and validation are defined
once in `internal/config/accounts.go`. Chrome gets an OS-assigned debugging
port passed explicitly, never Chrome's `--remote-debugging-port=0` automation mode.

For Flow, the session values (build label, CSRF token, language, User-Agent,
cookies) come from the account's Flow page; RPC IDs are discovered from the
compiled scripts of the running build, and the balance is read through the page.
A balance error keeps the account signed in and is shown on its card.

Regression tests use synthetic sessions and local CDP/HTTP responses. To run the
optional real Chrome lifecycle smoke test with a temporary profile and
`about:blank`, set `SNAKEMEDIA_TEST_CHROME=1` and run
`go test ./internal/browser -run TestBrowserLifecycle -v`. This does not test an
interactive sign-in.

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

Upload retry policy and polling backoff live under `flow` in `config.json`.
Flow's own limits (source size, trim minimum and step, accepted types) are not
settings; those Flow does not expose are named fallbacks in
`internal/providers/flow/limits.go`, logged on first use.
Script discovery has a separate `flow.maxDiscoveryBytes` budget, so studio
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

`SNAKEMEDIA_LIVE_APP` runs the whole app on the real data folder and accounts
(`go test . -run '^TestLiveApp$' -v -count=1`); its value lists the steps:
`list`, `rpcs`, `credits` (free), `flow-image`, `flow-video` (cheapest video,
wait cancelled midway then resumed), `muse-image`, `muse-video`. The paid steps
spend real credits or quota. `SNAKEMEDIA_LIVE_DIRECT` lists account IDs to set
to "no proxy" first.

## Flow image queue

**Flow > Ảnh > Hàng loạt** queues prompts in Go (`flow/image-queue.json`), runs
them with the thread count and spacing of **Cài đặt Flow**, and saves results to
the chosen folder. Each task writes its request ID before sending, so after a
restart a running task is checked again under that ID, never sent again.

## Muse

The **Muse** tab queues image and video jobs on muse.ai and spreads them over
the Muse accounts of **Trang chủ > Tài khoản**; the queue is journaled in
`muse/jobs.json`. Made files are also recorded in the library
(`muse/library.json`), which keeps them when finished jobs are cleared from the
queue.

A job that Muse already accepted is only checked again in its own chat, also
after a restart, so an interruption never spends a second generation. Timing,
retry and thread limits live under `muse` in `config.json` (**Muse > Cài đặt
Muse**). What Muse itself allows (weekly quota and additional tokens, accepted
reference images and their sizes, models) is read from muse.ai when an account
is checked and is never configured in the app (ADR 0011).

To open muse.ai anonymously in a real Chrome and check the page script, run
`SNAKEMEDIA_TEST_MUSE=1 go test ./internal/providers/muse -run TestLiveAnonymousPage -v`.
