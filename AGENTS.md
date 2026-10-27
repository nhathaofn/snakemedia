# SnakeMedia

Production desktop application scaffold built with Go and Wails v2, using Vite + React 19 + TypeScript for the presentation layer.

## Stack
- Backend: Go 1.26 (see `go.mod`), Wails v2.17.0 runtime
- Frontend: Vite 7, React 19, TypeScript 5, React Router 7, Zustand 5
- Tooling: ESLint 10 (typescript-eslint, react-hooks), Prettier 3, Vitest 5
- Package manager: npm 11

## Commands
| Task | Command |
|---|---|
| Install dependencies | `npm --prefix frontend install` |
| Dev (live reload & HMR) | `wails dev` |
| Regenerate TS bindings only | `wails generate module` |
| Test (Go) | `go test ./...` |
| Vet (Go) | `go vet ./...` |
| Format check (Go) | `go run ./tools/gofmtcheck` (fix with `gofmt -w .`) |
| Test (Frontend) | `npm --prefix frontend run test` |
| Lint (Frontend) | `npm --prefix frontend run lint` |
| Format (Frontend) | `npm --prefix frontend run format` / `format:check` |
| Build (Frontend static assets) | `npm --prefix frontend run build` — required before any Go command, `main.go` embeds `frontend/dist` |
| Build (Go backend) | `go build ./...` |
| Build (Full Desktop Executable) | `wails build` (optionally `-ldflags "-X SnakeMedia/internal/buildinfo.buildDate=<date>"`) |

## Repo map
| Path | Responsibility | Details |
|---|---|---|
| `wails.json` | **Single source of truth** for app name, product name, company and version (`info.productVersion`) | Embedded by `main.go` |
| `main.go` | Entry point: parses manifest, builds window options from config, lifecycle hooks, single-instance lock, enum bindings | — |
| `app.go` | Wails-bound controller; every exported method is exposed to the frontend | — |
| `internal/buildinfo/` | App identity and build metadata parsed from `wails.json` | `buildinfo.go` |
| `internal/paths/` | Per-user data, config, log, media, accounts and default image output locations | `paths.go` |
| `internal/config/` | User settings (`Theme`, `LogLevel`, `WindowScale`, shared account/browser policy, Flow and Muse policy): validation, atomic persistence, corrupt-file quarantine | `config.go`, `accounts.go` |
| `internal/window/` | Startup window geometry: `WindowScale` × primary screen, clamped to min size and screen | `sizing.go` |
| `internal/logger/` | `log/slog` logger: file + stdout, size rotation at startup, runtime level changes; `Recover` for background goroutines | `logger.go`, `recover.go` |
| `internal/events/` | Typed IPC event names (`events.Event`) and dispatcher | `events.go` |
| `internal/models/` | `models.Response[T]` envelope and `models.Code` result codes | `response.go` |
| `internal/services/` | App services (`SystemService`, `SettingsService`) | — |
| `internal/engine/` | Node specs (typed ports, settings), registry, typed node adapter, `MediaRef` (the value of image/video/media ports) and `Compatible`, option sources (`OptionsFrom`), `PendingError`, graph executor (`RunGraph`) and run keys that make re-runs resume instead of resend | `node.go`, `graph.go`, `media.go`, `options.go`, `runkey.go` |
| `internal/media/` | Downloaded and imported media store, `/flow-media/` middleware, streaming `SaveTo`, allowed output roots (`roots.go`), `media.save` node | `store.go`, `roots.go` |
| `internal/browser/` | Chrome for every provider: launcher (job object), one-browser-per-profile `Pool`, CDP calls (`Call`, `Evaluate`, `Conn`), tabs, cookie import | `pool.go`, `cdp.go`, `tab.go` |
| `internal/proxy/` | Shared proxy list (encrypted), parsing, health checks, rotating providers, local bridge, per-account `Route` | `manager.go`, `proxy.go`, `bridge.go` |
| `internal/accounts/` | Every provider's accounts: encrypted store, sign-in window or cookies, checks, proxy assignment, removal; a provider plugs in a `Site` and keeps its data in `Record.Data` | `service.go`, `api.go`, `site.go` |
| `internal/jobs/` | Generic durable journal for queues: index by ID, atomic writes, transient (unsaved) progress, trim of finished records only, corrupt-file quarantine | `journal.go` |
| `internal/providers/` | `Provider` interface and the plugin set (`Plugin{Site, New}`, `Prepare`/`Build`/`Register`/`Start`/`Close`); one package per external service | `provider.go` |
| `internal/providers/flow/` | Google Flow provider: rotation, projects, model catalogue, image/video generation (shared billable entry in `billable.go`), video edit, job journals (cached), the durable image batch queue (Ảnh → Hàng loạt), platform limits not exposed by Flow as logged fallbacks (`limits.go`), nodes | `provider.go`, `billable.go`, `generation_jobs.go`, `image_queue.go`, `limits.go`, `edit.go`, `nodes.go` |
| `internal/providers/flow/session/` | Flow's `accounts.Site` (identity, session values, balance) and the account view the Flow provider rotates over | `site.go`, `service.go` |
| `internal/providers/flow/chrome/` | Flow protocol over the account's page: session inspection, RPC discovery, HTTP client, Scotty video upload, reCAPTCHA (do not edit recaptcha.go) | `session.go`, `batch.go`, `upload.go` |
| `internal/providers/muse/` | Muse provider: `accounts.Site` reading quota and limits from muse.ai (`site.go`), image/video job queue (on `internal/jobs`) and scheduler, chat-client runner, page script (`page.js`), library of made files kept apart from the queue (`library.go`), ratios and video lengths asked in the message and checked on the made file (`format.go`), engine nodes | `provider.go`, `runner.go`, `scheduler.go`, `page.go`, `protocol.go`, `library.go`, `nodes.go` |
| `internal/secure/` | DPAPI protection for local stores (accounts, proxy list); `ErrOtherUser` when data was sealed for another Windows user | `protect_windows.go`, `secure.go` |
| `pkg/utils/` | Reusable utilities (atomic writes, file checks, byte formatting) | `fileutils.go` |
| `tools/gofmtcheck/` | Cross-platform gofmt gate used by CI and verify | — |
| `frontend/src/components/` | UI primitives (`ui/`, incl. `Modal`, `RevealList`), desktop shell (`layout/`: TitleBar, Header, Sidebar), feedback (`feedback/`), settings rows (`settings/`: section, number/text/toggle rows, "Nâng cao"), shared batch form and the one prompt split rule (`batch/`), the studio frame both providers use (`studio/`: `StudioLayout` with Đơn / Hàng loạt tabs, `PromptBar`), `accounts/AccountWarning` | `batch/prompts.ts`, `studio/StudioLayout.tsx` |
| `frontend/src/features/` | One folder per header section (`home/`: accounts of every provider, proxy list; `flow/`; `muse/`; `settings/`): `section.tsx` manifest + one sub-folder per feature (Flow: `image/` with `all/` rendering the Go image queue, `video/`, `edit/`, `settings/`, shared UI, `useStudioSubmit`, generation and job recovery hooks in `shared/`; Muse: `MuseStudio` per kind with `single/` (gallery + prompt bar) and `batch/` (batch form + queue), a job's `mode` deciding the screen, shared panel fields and actions in `shared/`, `library/`, `settings/`) | `<section>/section.tsx`, `flow/shared/recovery.ts` |
| `frontend/src/routes/` | Section registry (`registry.ts`) → router, header tabs, per-section sidebar; `useActiveSection` | `registry.ts` |
| `frontend/src/stores/` | Global state with Zustand (`useAppStore`, `useAccountsStore`) | `appStore.ts`, `accountsStore.ts` |
| `frontend/src/services/` | Typed bridge over generated bindings (`api`, `invoke`, `unwrap`), runtime detection | `api.ts` |
| `frontend/src/hooks/` | `useAppBootstrap`, `useThemeSync`, `useWailsEvent`, `useAccountsLive`, `useConfigSection` / `useConfigUpdate` (revisioned saves, retry on conflict), `useCompactLayout`, `useNow` | — |
| `frontend/src/constants/` | UI constants (toast duration, theme labels) | `ui.ts` |
| `frontend/src/styles/` | Design tokens (`tokens.css`) and global styling (`globals.css`) | — |
| `frontend/src/types/` | `backend.ts` re-exports generated DTOs/enums; `app.ts` holds frontend-only types | — |
| `frontend/wailsjs/` | Auto-generated Go bindings and runtime bridge | Auto-generated |
| `.agents/verify.json` | Verification gate commands for fast pre-completion checks | — |
| `.github/workflows/` | Continuous integration pipeline configuration | `ci.yml` |

## Conventions
- **Zero hardcode:** identity/version come from `wails.json`; user-tunable values live in `internal/config`; shared enums (theme, log level, events, response codes) are Go types bound via `EnumBind` and consumed in TS through `frontend/src/types/backend.ts`. Never redeclare a backend DTO or enum in TypeScript.
- Native system interactions, OS file access, and heavy background tasks belong in Go: external services in `internal/providers/<name>/` (exposed as engine nodes), generated files in `internal/media/`, app-level services in `internal/services/`.
- Go methods exposed to the frontend return `models.Response[T]` with a `models.Code`; call them only through `frontend/src/services/api.ts`.
- Navigation: header tabs are `AppSection`s; the sidebar lists only the active section's features, with `placement: 'sidebar'` sections (Settings) pinned at its bottom. Manifests are `as const satisfies AppSection`; navigate with the typed `ROUTES` map (`ROUTES.home.accounts`), never with path string literals. A page that lays out its own panels sets `layout: 'full'` on its feature instead of adding feature selectors to layout CSS. New screen: add one feature to `frontend/src/features/<section>/section.tsx`. New header tab: create that manifest and add it to `APP_SECTIONS` in `frontend/src/routes/registry.ts`. Router, header and sidebar derive from it.
- Colours, sizes and timings in CSS come from `frontend/src/styles/tokens.css`.
- The main window starts hidden; `App.domReady` sizes it to `WindowScale` (default 0.7) of the primary screen, centres it, then shows it. Never remove `StartHidden` without moving that logic.
- `go.mod` has `ignore ./frontend/node_modules` so `./...` never picks up Go code shipped inside npm packages.
- `.gitattributes` keeps LF in the working tree on every OS; gofmt and Prettier fail on CRLF.
- Billable Flow requests carry a request ID that is also the conversation turn ID. Reuse an ID only when the previous attempt's outcome is unknown (bridge failure or pending job); a definite answer from Flow means the next attempt gets a new ID.
- Flow RPC IDs are discovered by method name from the running build (`chrome/resolver.go`); project-only methods need `projectPageClient`. A hardcoded ID is allowed only as a logged fallback.
- Accounts, their Chrome profiles, cookies and proxies belong to `internal/accounts` for every provider. A provider adds an `accounts.Site` and keeps its own runtime data in `Record.Data`; never a second account store, browser pool, CDP client or proxy path (ADR 0010).
- Every account browser runs on the account's proxy route, and a provider's own HTTP clients use the same route (`ProxyURL`); a failing proxy stops the account, never falls back to the real IP. Only "Không dùng proxy" (`proxy.None`) is direct: an empty proxy list fails for every other account, `ProxyURL` returns an error when no route is recorded (never nil for "unknown"), and a signed-in account is never moved to another proxy line on its own.
- Settings screens are built from `components/settings`: a few main rows per group, rarely changed values under "Nâng cao". Shared browser policy is edited in Cài đặt → Chung, provider policy in that provider's own settings screen.
- Chrome launched by the app is tied to the app process (Windows job object), so a crash or dev reload does not leave a signed-in browser behind.
- Every background goroutine starts with `defer logger.Recover(where, onPanic)`: a panic is logged with its stack instead of ending the app. Long-lived loops recover per iteration (`safePump`); a job that panicked settles as an unknown outcome (Flow keeps its request ID, a Muse job caught while sending fails with `errSendUnknown`), never as "not sent".
- Shutdown is bounded: providers get `closeWait` in all (`internal/providers`), accounts and browsers theirs; unfinished jobs stay in their journals. A failed start shows a native error box (`fatal_windows.go`); a config reset to defaults is shown once the window is up.
- Video metadata for MP4/MOV/M4V is read from the file header (`internal/media/mp4probe.go`); ffprobe is only needed for WebM or unusual files.
- `wails dev` builds run without the single-instance lock (`instance_lock_dev.go`), so a rebuild never hands off to the stale window.
- A Muse job with a `SessionID` is only re-checked in that chat, never re-sent; a send whose answer was lost fails instead of retrying (ADR 0008). An unknown outcome (missing session ID, unknown status, missing file, transient RPC/listing error) is always re-checked, never treated as "not sent" (ADR 0012).
- After a sign-in in the account's Chrome window (not a plain check), a site implementing `accounts.SignInFollowUp` runs its own step before the account shows as signed in; its failures are only noted on the account (`Inspection.Note`). Muse uses it to send `muse.setupMessage` (Cài đặt Muse → Tài khoản) in a chat of its own, once per account (a message Muse took is never sent again), and waits for Muse's reply (`muse/setup.go`).
- Durable queues and journals use `internal/jobs` (Flow image queue, Muse queue and library); a queue task writes its request ID to disk before the request can leave the app.
- Every batch form splits prompts with `components/batch/prompts.ts` in the mode the user picks (Từng dòng: one prompt per line; Theo đoạn: blocks between blank lines; Tổng hợp: the whole text is one prompt); the last mode is remembered in localStorage. Flow Ảnh → Hàng loạt fixes the mode to Từng dòng (`promptMode`). Enter (or a .txt import) adds the prompts to the queue on hold, skipping prompts already waiting (`newPrompts`); Ctrl+Enter breaks the line; the screen's "Chạy" starts held prompts and "Dừng" holds the waiting ones (Flow and Muse alike).
- Files saved for the user are named `<n>-snakemedia-<provider>-<6 random chars>` (`media.OutputName`): n is the prompt's place in its batch, or the folder's next number for a file saved by hand. Batch forms pick the folder with `components/batch/OutputFolderField` (remembered per provider in config: `flow.batchOutputDir`, `muse.outputDir`); each batch (Flow Ảnh → Hàng loạt, Muse Hàng loạt) saves into its own sub-folder `snakemedia-<provider>-<kind>-<date-time>` of that folder, and prompts added while it is unfinished join it with the next numbers (`media.BatchFolder`); Muse images (made as WebP) are converted on save to `muse.imageFormat` (PNG, JPG or original; `media.ConvertImages`), the media folder keeps the original; single studios save nothing on their own, only through their download button.
- Platform limits Flow does not expose live in `internal/providers/flow/limits.go` as named fallbacks logged on first use; never in config or TypeScript.
- Fast checks defined in `.agents/verify.json` must pass before finishing any development task.

## Boundaries
- Do not check in build artifacts (`build/bin/`, `frontend/dist/`, `node_modules/`, `*.exe`).
- Do not edit `frontend/wailsjs/` manually; regenerate with `wails dev`, `wails build` or `wails generate module`.

## Key decisions
- 0001 Desktop Scaffold Stack: Go + Wails v2 + Vite (React + TS) → docs/decisions/0001-desktop-scaffold-stack.md
- 0002 Production Modular Architecture and Dynamic Full Scaffolding → docs/decisions/0002-production-modular-architecture.md
- 0003 Single Source of Truth and Production Hardening → docs/decisions/0003-single-source-of-truth-and-hardening.md
- 0004 Sectioned Navigation: Header Tabs Own the Sidebar → docs/decisions/0004-sectioned-navigation.md
- 0005 Flow Image reCAPTCHA Enterprise Token Minting and Image-to-Image Pipeline → docs/decisions/0005-flow-image-recaptcha-and-image-to-image.md
- 0006 Providers, Engine Nodes and a Shared Media Store → docs/decisions/0006-provider-engine-modules.md
- 0007 Flow Video Editing: Scotty Upload and jIps6 → docs/decisions/0007-flow-video-edit.md
- 0008 Muse Provider: Chat-Client Jobs, a Durable Queue and Many Accounts → docs/decisions/0008-muse-provider.md
- 0009 Muse Queue Controls, Proxy Bridge, Studio Assets & Multi-Scene Production → docs/decisions/0009-muse-queue-proxy-studio-production.md
- 0010 One Account Service for Every Provider: Shared Browser, CDP and Proxy → docs/decisions/0010-shared-accounts-browser-proxy.md
- 0011 Muse: Base Image/Video Generation, Limits Read from Muse → docs/decisions/0011-muse-base-media-from-platform-config.md
- 0012 Production Hardening: Pay Once, Fail Closed, Durable Queues, Composable Nodes → docs/decisions/0012-production-hardening-pay-once-fail-closed.md

## Deeper docs
- Decisions: `docs/decisions/README.md`
