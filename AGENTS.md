# SnakeMedia

Production desktop application scaffold built with Go and Wails v2, using Vite + React 19 + TypeScript for the presentation layer.

## Stack
- Backend: Go 1.26 (see `go.mod`), Wails v2.16.0 runtime
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
| `internal/paths/` | Per-user data, config and log locations | `paths.go` |
| `internal/config/` | User settings (`Theme`, `LogLevel`, `WindowScale`): validation, atomic persistence, corrupt-file quarantine | `config.go` |
| `internal/window/` | Startup window geometry: `WindowScale` × primary screen, clamped to min size and screen | `sizing.go` |
| `internal/logger/` | `log/slog` logger: file + stdout, size rotation at startup, runtime level changes | `logger.go` |
| `internal/events/` | Typed IPC event names (`events.Event`) and dispatcher | `events.go` |
| `internal/models/` | `models.Response[T]` envelope and `models.Code` result codes | `response.go` |
| `internal/services/` | Domain services (`SystemService`, `SettingsService`) | — |
| `pkg/utils/` | Reusable utilities (atomic writes, file checks, byte formatting) | `fileutils.go` |
| `tools/gofmtcheck/` | Cross-platform gofmt gate used by CI and verify | — |
| `frontend/src/components/` | UI primitives (`ui/`), desktop shell (`layout/`: TitleBar, Header, Sidebar), feedback (`feedback/`) | — |
| `frontend/src/features/` | One folder per header section (`home/`, `flow/`, `settings/`): `section.tsx` manifest + one sub-folder per feature | `<section>/section.tsx` |
| `frontend/src/routes/` | Section registry (`registry.ts`) → router, header tabs, per-section sidebar; `useActiveSection` | `registry.ts` |
| `frontend/src/stores/` | Global state with Zustand (`useAppStore`) | `appStore.ts` |
| `frontend/src/services/` | Typed bridge over generated bindings (`api`, `invoke`, `unwrap`), runtime detection | `api.ts` |
| `frontend/src/hooks/` | `useAppBootstrap`, `useThemeSync`, `useWailsEvent`, `useDebounce` | — |
| `frontend/src/constants/` | UI constants (toast duration, theme labels) | `ui.ts` |
| `frontend/src/styles/` | Design tokens (`tokens.css`) and global styling (`globals.css`) | — |
| `frontend/src/types/` | `backend.ts` re-exports generated DTOs/enums; `app.ts` holds frontend-only types | — |
| `frontend/wailsjs/` | Auto-generated Go bindings and runtime bridge | Auto-generated |
| `.agents/verify.json` | Verification gate commands for fast pre-completion checks | — |
| `.github/workflows/` | Continuous integration pipeline configuration | `ci.yml` |

## Conventions
- **Zero hardcode:** identity/version come from `wails.json`; user-tunable values live in `internal/config`; shared enums (theme, log level, events, response codes) are Go types bound via `EnumBind` and consumed in TS through `frontend/src/types/backend.ts`. Never redeclare a backend DTO or enum in TypeScript.
- Native system interactions, OS file access, and heavy background tasks belong in `internal/services/` in Go.
- Go methods exposed to the frontend return `models.Response[T]` with a `models.Code`; call them only through `frontend/src/services/api.ts`.
- Navigation: header tabs are `AppSection`s; the sidebar lists only the active section's features, with `placement: 'sidebar'` sections (Settings) pinned at its bottom. Manifests are `as const satisfies AppSection`; navigate with the typed `ROUTES` map (`ROUTES.home.accounts`), never with path string literals. New screen: add one feature to `frontend/src/features/<section>/section.tsx`. New header tab: create that manifest and add it to `APP_SECTIONS` in `frontend/src/routes/registry.ts`. Router, header and sidebar derive from it.
- Colours, sizes and timings in CSS come from `frontend/src/styles/tokens.css`.
- The main window starts hidden; `App.domReady` sizes it to `WindowScale` (default 0.7) of the primary screen, centres it, then shows it. Never remove `StartHidden` without moving that logic.
- `go.mod` has `ignore ./frontend/node_modules` so `./...` never picks up Go code shipped inside npm packages.
- Fast checks defined in `.agents/verify.json` must pass before finishing any development task.

## Boundaries
- Do not check in build artifacts (`build/bin/`, `frontend/dist/`, `node_modules/`, `*.exe`).
- Do not edit `frontend/wailsjs/` manually; regenerate with `wails dev`, `wails build` or `wails generate module`.

## Key decisions
- 0001 Desktop Scaffold Stack: Go + Wails v2 + Vite (React + TS) → docs/decisions/0001-desktop-scaffold-stack.md
- 0002 Production Modular Architecture and Dynamic Full Scaffolding → docs/decisions/0002-production-modular-architecture.md
- 0003 Single Source of Truth and Production Hardening → docs/decisions/0003-single-source-of-truth-and-hardening.md
- 0004 Sectioned Navigation: Header Tabs Own the Sidebar → docs/decisions/0004-sectioned-navigation.md

## Deeper docs
- Decisions: `docs/decisions/README.md`
