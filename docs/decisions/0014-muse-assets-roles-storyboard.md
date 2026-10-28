# 0014. Muse Asset Library, Reference Roles, Storyboard and Source Video

- **Status:** Accepted
- **Date:** 2026-10-28
- **Amends:** ADR 0008 (what a job attaches), ADR 0012 (a job that waits for another)

## Context
Characters and products drifted between Muse jobs: a face changed between
prompts of one batch, a product lost its printed text. The two reference apps
next to this repo (`../muse/Musee-*`, `../muse/HaoHao-Project`) handle this
with (a) a library of tagged reference images with a **role** (face, full
character, storyboard, product) and **lock notes**, written into the message
as a numbered role and priority block; and (b) a **storyboard** step: Muse
first draws the opening frame of every scene, then makes the video from those
frames. HaoHao measured it on muse.ai (2026-10-09): a direct video changed the
person, room and product text between scenes; the storyboard kept them.

## Decision
Everything here is additive: a request that uses none of it, a graph run, and
every job in an older journal build the same message and attachments as before.

1. **Asset library** (`assets.go`): `MuseAsset{Tag, Name, Role, Lock, Images≤3}`
   in `muse/assets.json` and saved prompts in `muse/templates.json`, both on
   `internal/jobs`. Images are media files named `muse-asset-*`, removed only
   when no asset and no job uses them.
2. **Per-prompt images are decided at submit** (`planPrompts` in `submit.go`,
   `compose.go`; one submit loop for every request, graph runs attaching only
   their own images):
   fixed assets (`MuseJobRequest.Assets`) → assets the prompt names (`@tag`,
   any case, or the exact name as a whole word, in order of appearance) → the
   batch's own images by `RefMode` (shared, pair: prompt n gets image n,
   rotate: one each in turn). The result is written into the job
   (`References`, `RefInfo`), so a job sent again attaches the same images
   whatever the library holds later.
3. **Role block** (`roleLines`) replaces the plain Reference lines when the
   job's images carry roles: one line per numbered image with its rule,
   priority (product > storyboard > prompt; identity images > storyboard
   faces > prompt), and the locks. Hints (`CheckPrompt`) never block a job.
4. **Storyboard** (`storyboard.go`): a video prompt becomes two ordinary jobs.
   The image job (`BoardFrames`) asks for the frames; the video job waits for
   it (`After`, `waitsForBoard` in `plan`) and, once it succeeded, attaches its
   images as `Frames` before its own references, **in a new chat**. Sending
   into the storyboard's chat would need a second send into a paid chat; two
   independent jobs keep "never pay twice" with no new rules. A video with its
   frames no longer needs the storyboard job, which may be cleared. With
   review (`Review`) the video is held again when its frames arrive, even if
   the batch was run before, so the user runs it after seeing them. Both jobs
   show on the Video screen (`screenKind`).
5. **Source video (v2v)**: a video imported with `ImportSourceVideo` (types
   read from Muse's composer, `VideoTypes`) is attached first and the message
   asks Muse to study, then recreate, its movement. One message, one send.

## Consequences
- The storyboard video is made in a new chat with the frames re-attached (the
  case HaoHao had not tried); if it holds less well than in the same chat,
  sending into that chat is a later change with its own pay-once rules.
- Video upload depends on Muse listing a video type for the composer; until
  an account's limits are read again, v2v says so instead of sending.
