# 0007. Flow Video Editing: Scotty Upload and jIps6

- **Status:** Accepted
- **Date:** 2026-10-23
- **Updated:** 2026-10-24

## Context
Omni can rewrite an existing video from a prompt (Flow's `EDIT_VIDEO` mode). A
live capture of the upload and of RPC `jIps6` was checked against the Flow web
bundle (`chunk_studio_1.js`). The capture was right about the endpoint, the
upload reply and the request layout, and wrong in three details:

- the upload is not one POST: Flow's Scotty uploader uses the resumable
  protocol (`X-Goog-Upload-Command: start`, then `upload, finalize` on the
  returned `X-Goog-Upload-URL`);
- `X-Framework-Xsrf-Token` is the `XSRF-TOKEN` cookie, not the `at` token, and
  is only sent when that cookie exists;
- the trailing `[4]` of the request item is field 13, the resolution (4 = 360p),
  not the duration. It is omitted for 720p.

## Decision
1. `chrome.FlowClient.UploadVideo` sends the file to
   `/upload/v1/flow/upload/video/{projectID}` with the resumable protocol, the
   `Slug` and optional `x-trim-start`/`x-trim-end` (ms) headers, configurable retry and size policy (including `Retry-After`), and reads
   `mediaId` from the reply as Flow does.
2. Like Flow's editor (`generate_video_into_workflow`: `count: 1`, ratio and
   resolution taken from the source media), an edit makes one video and has no
   ratio, quality or count setting. The ratio is the nearest Flow ratio to the
   uploaded video's size (`_.TO`, then `SO`: 16:9 and 4:3 are landscape, 9:16
   and 3:4 portrait, 1:1 has no edit variant). An uploaded video is 720p in
   Flow (`_.CP` resolution 1), so `abra_edit` is used, with `abra_edit_360p`
   only when the tier lacks the 720p usage. Edit usages are those whose
   capabilities include 20 (video input); the longest source they read is
   HTrJv field 22 slot 4 (10 s by default).
3. The `jIps6` item is `[[null, mediaID, startFrame, endFrame], prompt,
   variantKey, aspect, clientIDs, …, [resolution]]`. The frame range is computed from the selected interval and the actual
   source duration after Flow processing, capped by the selected usage input limit.
   Local files are validated with ffprobe before upload. A successful upload
   starts asynchronous source processing; the backend waits for successful
   status and a serving URL before editing. Sources are uploaded untrimmed;
   coordinates refer to Flow's normalized 30 fps timeline and metadata.
   Edit metadata targets the uploaded workflow
   rather than allocating another workflow.
4. The reply has the same media layout as text-to-video. Tracking uses Flow's
   `BatchCheckAsyncVideoGenerationStatus` with media records in field 3, as the
   web client's `gcb` loop does. GetMedia is only a serving-URL fallback and the
   project listing resolves submissions whose response was lost.
   The existing reCAPTCHA mint block remains unchanged. Its site key is
   discovered from the account's loaded page, with no captured-key fallback.
   The token is minted after upload; the edit is submitted from that same
   browser session. RPC IDs are discovered from its active build and lazy
   studio scripts.
5. Source videos are picked with the native file dialog (`App.PickVideo`) and
   copied into the media store, so the webview previews them from
   `/flow-media/` and no file bytes cross the Wails bridge.
6. The Flow section gets an **Edit video** tab with its own layout: source
   preview, trim and model on the left, results and prompt on the right.
   Results reuse the shared `VideoCard`.
7. The backend persists opaque source ownership and a mutation journal before
   submission. Sources bind account/project/workflow/media IDs; frontend input
   cannot replace them. Pending jobs survive timeout/restart and can be resumed
   without another upload or edit submission. The Edit video gallery keeps
   results only for the current app session and does not restore the saved
   journal automatically at startup. Completed files are streamed atomically
   to disk and checked using real media metadata.

## Consequences
- The edit node `flow.video.edit` takes a stored video URL as its input port.
- Image references for edits (field 9) and extend/upsample modes are not wired;
  they can be added on the same request builder once captured.
