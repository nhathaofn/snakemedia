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
