# 0003. Single Source of Truth and Production Hardening

- **Status:** Accepted
- **Date:** 2026-10-23
- **Spec:** —

## Context
A review of the 0002 scaffold found that the structure was sound but values were duplicated or hardcoded across layers: app name in six places, version stored in the user's config file, window size and theme defined but never applied, event names and DTOs declared twice (Go and TypeScript), and navigation declared separately from routes. Production concerns were also missing: logs went only to stdout (invisible in a Windows GUI build), config writes were not atomic, corrupt config was silently ignored, and CI failed on a clean checkout because `//go:embed frontend/dist` ran before the frontend build.

## Decision
1. **Identity:** `wails.json` (`name`, `info.productName`, `info.productVersion`, `info.companyName`) is the only place the app name and version are written. `main.go` embeds it; `internal/buildinfo` parses it. Commit comes from Go VCS stamping, overridable with `-ldflags`.
2. **Configuration:** `config.AppConfig` contains only user-tunable settings, uses typed enums, is validated on load and update, written atomically, and moved aside (`config.json.corrupt-<ts>`) when unreadable. Theme is applied at startup. Window size is relative, not absolute: the window starts hidden and is sized to `windowScale` (default 0.7) of the primary screen in logical pixels, clamped to the minimum size and to the screen, centred, then shown.
3. **Shared contract:** Go enums (`config.Theme`, `config.LogLevel`, `events.Event`, `models.Code`) are bound with Wails `EnumBind`. The frontend imports generated types through `src/types/backend.ts` and calls Go only through `src/services/api.ts`, which wraps the generated bindings.
4. **Navigation:** `src/routes/registry.tsx` is the single list of screens; router and sidebar are derived from it.
5. **Runtime:** frameless window with the custom title bar, single-instance lock, `OnDomReady`/`OnShutdown` hooks, file logging under the user config dir with size rotation and runtime level changes.
6. **Quality gates:** CI builds the frontend first, then runs ESLint, Prettier, Vitest, a cross-platform gofmt check, `go vet`, `go test`, and `go build`. The same commands are in `.agents/verify.json`.

## Consequences
- Renaming or versioning the app is a one-line change in `wails.json`.
- Changing a Go DTO or enum surfaces as a TypeScript compile error after bindings regenerate.
- Adding a screen is one registry entry.
- `frontend/dist` must exist before Go commands; local workflows and CI build the frontend first.
- The window background colour in `main.go` must be kept in sync with `--bg-primary` in `tokens.css` (it is painted before the webview loads).
