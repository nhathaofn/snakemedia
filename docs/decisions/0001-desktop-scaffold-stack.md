# 0001. Desktop Scaffold Stack with Go, Wails v2, and Vite + React + TypeScript

- **Status:** Accepted
- **Date:** 2026-10-20
- **Spec:** —

## Context
The user requested a blank desktop application scaffold built with Go and Wails + Vite, with no business or UI content yet. The technical choices for desktop runtime and frontend bundling needed to be established cleanly for future development.

## Decision
1. Use Go (v1.26.5) with Wails CLI (v2.16.0) as the native desktop wrapper and backend runtime.
2. Use Vite (v7) with React (v19) and TypeScript (v5) for the frontend (`frontend/` directory).
3. Keep the initial codebase strictly as a clean skeleton: remove sample greeting forms, logos, and greeting methods in `app.go` and `App.tsx`, maintaining only the base lifecycle and layout shell.
4. Establish verification gates in `.agents/verify.json` with `go build ./...`, `go vet ./...`, `go test ./...`, and `npm --prefix frontend run build`.

## Alternatives considered
- Vue + TypeScript: Great reactivity, but React + TypeScript was selected as the standard matching user preference.
- Svelte / Vanilla: Lacks the extensive ecosystem and component libraries available for React when scaling later.

## Consequences
- Clean separation between Go native system capabilities and Vite frontend UI.
- Type-safe IPC bindings automatically generated in `frontend/wailsjs`.
- Fast local development cycle using `wails dev` with Vite HMR.
