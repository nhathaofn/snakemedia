# 0009. Muse Provider: Advanced Queue Controls, Proxy Bridge, Studio Assets & Multi-Scene Production

- **Status:** Partly superseded by ADR 0010 (proxy, accounts) and ADR 0011 (studio, V2V, scripts, productions)
- **Date:** 2026-10-26

## Context
Following Phase 1 (ADR 0008), the Muse Provider required advanced automation capabilities:
1. Queue management (pausing jobs on hold, bulk operations, project pause filtering, thread pinning).
2. Proxy support across static and rotating Vietnamese proxy providers without leaking the real machine IP.
3. Studio tagging and reference composition (@tag mentions, reference manifests, V2V workflows).
4. Multi-scene script authoring, storyboard generation, FFmpeg video merging, and background music integration.

## Decision

### 1. Queue Controls & Account Pinning
- Extended `MuseJob` with `Held`, `ProjectID`, `Label`, `Manifest`, `SourceVideo`, `KeepAudio`, `PinAccount`, `Thread`.
- The scheduler `plan` respects paused projects and held jobs. Jobs with a fixed thread and `PinAccount` are strictly delegated to their pinned account.
- Implemented batch APIs: `RunJobs`, `HoldQueued`, `RetryFailed`, `ClearFinished`, `RemoveJobs`, `EditAndRetry`, `OpenOutputFolder`, and `RevealFile`.

### 2. Proxy Architecture & Local Bridge
- **Storage:** Persisted at `muse/proxies.json` encrypted via Windows DPAPI (`internal/secure`), preventing plaintext password leaks.
- **Parsing:** `ParseProxy` recognizes static HTTP/SOCKS5 formats, rotating provider keys (`proxyvn:`, `tmproxy:`, `shoplike:`), and API rotation URLs.
- **Local Bridge:** Chrome instances are launched with `--proxy-server=http://127.0.0.1:<port>`. A local bridge listener forwards HTTP CONNECT and SOCKS5 authentication transparently to upstream proxies.
- **Safety guarantee:** If all proxies in the pool fail, jobs and sign-in fail immediately with descriptive errors rather than falling back to the machine's real IP.
- **Account Binding:** Accounts support sticky proxy assignments (`ProxyAuto`), manual proxy overrides, and on-demand IP rotation (`RotateAccountIP`).

### 3. Studio, Tagging & Reference Manifest
- **Studio Store:** Persisted at `muse/studio.json` supporting projects, tagged reference assets (`character_face`, `character_full`, `storyboard`, `product`), fixed model configs, and rotating batch image pools.
- **Composition (`composeImages`):** Automatically detects `@tag` mentions in prompt text, extracts image references in exact mention order, prepends fixed model images, inserts rotating batch images, deduplicates, and caps at `MaxReferences`.
- **System Manifest:** Prepends a strict system priority protocol block (`buildRefManifest`) enforcing character identity locks and product authority over storyboard drawings.
- **V2V (Video-to-Video):** Supports source video attachment with size checks, preset prompt templates ("Zoom áo", "Thay áo"), and audio preservation via ffmpeg extraction and remuxing.

### 4. Multi-Scene Production & FFmpeg Pipeline
- **Production State Machine:** Runs non-blocking inside `loop` via `pumpProductions`:
  1. *Storyboard generation:* Generates starting scene frames in an image job, binds thread session/account, and pauses in `review` state if configured.
  2. *Video generation:* Sends individual scenes (10s each with thread pinning) or combined video prompts using `script_prompt.go` formatting and natural speaking pace instructions (`PACE`).
  3. *Post-processing:* Merges individual scene outputs with FFmpeg concat (`-c copy` with automatic re-encoding fallback), mixes background music at 15% (under speech) or 80% (standalone) with fade in/out, and auto-saves to user media directory.

## Consequences
- SnakeMedia now has full feature parity with HaoHao-Project and Musee while guaranteeing DPAPI security, zero real-IP fallback leaks, and single-source-of-truth React UI.
- All 8 CI/verification checks pass cleanly: TypeScript build, ESLint, Prettier format, Vitest unit tests, gofmt, go vet, backend tests, and go build.
