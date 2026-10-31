# 0015. External Tools: One Manifest, Found First, Downloaded Only When a Feature Needs Them

- **Status:** Accepted
- **Date:** 2026-10-31

## Context
Features run programs the app does not ship: the Flow video watermark removal
needs ffmpeg and ffprobe, and reading a WebM's size needs ffprobe. Each place
looked them up its own way (`flow.ffmpegPath` / `flow.probePath`, then PATH),
the paths sat in Flow's settings although Muse uses ffprobe too, and the only
help was a dialog sending the user to a download page — shown as soon as the
app opened, even to someone who never made a video.

## Decision
1. **`internal/tools` is the only place that finds or installs an external
   program.** What exists is data in the embedded `manifest.json`: a tool's
   ID, name, programs and, per `GOOS/GOARCH`, its source (HTTPS archive URL,
   optional version URL, pinned `sha256` or the publisher's `sha256Url`,
   archive format, size cap). `{version}` and `{url}` are filled in at install
   time, so following upstream releases needs no code. Adding a tool is a
   manifest entry; callers name the program, never the tool.
2. **Lookup order** (`Service.locate`): the path the user set
   (`config.tools.paths[program]`, a broken one is reported, not skipped) →
   the folder of a sibling program the user set → the machine (`PATH`) → the
   app's own copy in `paths.ToolsDir` (`%LocalAppData%\SnakeMedia\tools\<id>`).
3. **Nothing is downloaded until a feature needs it and the user agrees.**
   `Service.Program(feature, program)` returns the path, or `ErrMissing` after
   sending `tools:missing` with the tool and the feature's name. The global
   `ToolPrompt` asks "Tải về" / "Để sau"; "Để sau" (`PostponeTool`) quiets that
   tool for the rest of the run. Unanswered asks and running downloads are
   kept in the service (`GetToolActivity`) so a window opened later still
   shows them. The feature itself carries on as it did without the tool
   (a video keeps its watermark).
4. **Install is verified and atomic:** download to a temp file under the
   tools folder with progress (`tools:progress`), check size and SHA-256,
   extract only the listed programs from the archive, write `tool.json`
   (version, URL, checksum, time), then replace the previous copy in one
   rename. A failed install leaves the old copy and no temp files. Downloads
   go direct, not over an account proxy: they belong to no account.
5. **Settings:** Cài đặt → Chung → Công cụ, one row per tool (status, "Tải về"
   when missing, "Gỡ" for the app's copy) and the per-program paths under
   "Nâng cao". `config.SchemaVersion` 2 moves `flow.ffmpegPath` /
   `flow.probePath` to `tools.paths` (absolute paths only).

## Consequences
- The installer stays small and the app ships no GPL/LGPL binaries; a user
  with FFmpeg on PATH sees no prompt and no download.
- A checksum file fetched next to the archive trusts the publisher's HTTPS
  host, not a value pinned in the app; a source that offers no checksum
  must pin `sha256`.
- Chrome stays outside this package: accounts need the user's real Chrome
  (Google sign-in, reCAPTCHA), found by `internal/browser`.
