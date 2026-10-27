# 0011. Muse: Base Image/Video Generation, Limits Read from Muse

- **Status:** Accepted
- **Date:** 2026-10-26
- **Supersedes:** ADR 0009 §3–§4 (studio assets, @tag manifests, V2V, scripts,
  storyboards, productions, FFmpeg merging) and the fixed ratio/count/size
  options of ADR 0008

## Context
The app is meant to be a production line for images and videos: each provider
offers its base generation features, and other features are composed on top
of them later. Muse had grown features on top of its queue (studio assets and
@tags, multi-scene scripts, storyboards, productions, FFmpeg merging,
video-to-video with "try-on" templates) while its base was built on guesses:

- The create screen offered aspect ratios and an image count, and the settings
  held "Muse's limit" on attachment size and a source-video size. Muse's own
  agent describes its tools as `media.generate_image` (prompt items of text or
  image, `output_format`, `style_preset`) and `media.generate_video` (prompt
  items only); neither takes a ratio, duration or count, and video-to-video is
  not supported.
- The quota was read through `fetchSubscriptionAction`, whose chunk muse.ai
  only loads with its settings screen, so the quota was never found from the
  home page ("Chưa đọc được").
- The page script lived under `window.__mss`; a tab that kept an older script
  across an app rebuild never got the new one.

## Decision
1. **Base only.** Muse jobs are `image` or `video`: one prompt, optional
   reference images, sent in its own new chat. Studio, scripts, productions,
   storyboards, V2V and FFmpeg are removed. Batch production is the queue:
   prompts × repeat count, spread over accounts by the scheduler.
2. **What Muse allows is read from Muse, per account** (`Inspect`, stored in
   the account's `museData`):
   - quota: the subscription (`fetchSubscriptionAction`): weekly usage,
     reset time, additional (top-up) tokens and Muse's own labels, shown on
     the account card as Muse words them;
   - capabilities: the composer's `DEFAULT_UPLOAD_CONFIG` (file size, files per
     message, accepted image types) and the account's models (`model.get`).
   `GetOptions` serves the most recently read capabilities; references are
   refused until they are known. Nothing about Muse's limits is in
   `config.MuseConfig`, which keeps only how the app runs its queue.
3. **Finding values in Muse's bundle** (`page.js` `scan`): loaded scripts
   first, then the lazy chunks they list (`static/chunks/*.js`); a found value
   is remembered per build in the tab's `localStorage`.
4. **The page script is keyed by its content** (`pageKey`, an FNV hash of
   `page.js`), so a changed script replaces an old one in an open tab.
5. **The prompt is sent unchanged** after a one-line request naming the kind
   (`kindRequest`); ratio or duration wishes belong in the prompt.
6. **Engine nodes** `muse.image.generate` and `muse.video.generate` queue the
   jobs, wait for them and return their files, so later features can chain
   Muse with other nodes without touching the queue.
7. Flow-only generation settings (generation and video timeouts, video
   polling, concurrent requests per account, signed-out reads) move from the
   shared `accounts` config to `flow`; the unused `refreshCooldownSeconds` is
   removed.

## Consequences
- An account must be checked once (Trang chủ → Tài khoản) before reference
  images can be added; the first read may scan Muse's scripts (seconds).
- Results are taken whole: every file of the job's kind in its chat.
- Old journals may hold `v2v` jobs; they fail with a clear message if run.
- Saved config values for the moved Flow fields fall back to their defaults
  once.
