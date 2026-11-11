# 0016. Muse Messages: a Short Brief for the Agent, the Prompt for the Scene

- **Status:** Accepted
- **Date:** 2026-11-11
- **Amends:** 0011, 0013, 0014 (message wording only; their flows are unchanged)

## Context
Every Muse job message had grown to ~4,500 characters of rules before the
user's prompt (6,800 for one clip with an opening frame, 10,500 for a 20 s
video): request rules, intent, authority, style, text, reference-sheet
handling, six one-clip rules, eleven long-video rules. A review by Muse
itself called the result contradictory (an unassigned single image was told
"do not copy framing" while the prompt said "start from this frame").

`TestLiveConsole` (new, opt-in) let us talk to Muse on the dev accounts and
ask, in the same chat, what was passed to its tool. Observed on 2026-11-11:

- Muse's agent calls `media.generate_video`, which takes only an ordered list
  of text and image paths (plus a name, output folder and snapshot to resume);
  no ratio, duration, audio or seed parameter. One call makes up to ~10 s.
- The agent **applies** our rules itself and passes the user's prompt (the
  scene) to the tool almost verbatim, adding its own wording (e.g. it spelled
  "static" out as "no push in, no zoom"). Our rule block never reaches the
  video model. `chat.history` holds no tool calls, so this rests on the agent's
  own account, checked against what the videos show.
- **No video-to-video and no image-to-video.** The tool definition says
  "Video-to-video input is not supported" and takes only text and still
  images; it has no "start on / end on this image" mode. Attached a video,
  the agent read its metadata, looked at three stills (0, 5, 9.9 s), wrote a
  new prompt and passed one still plus that text; it did not listen to the
  sound (the line came from the account's memory). Frame 0 of eight clips
  made with an "opening frame" matched that image at SSIM 0.30-0.69 (a literal
  first frame is above 0.9): images are references the scene is redrawn
  from.
- Muse remembers an account's earlier chats ("second take", "same as the
  earlier takes") despite the setup message.
- Test matrix: 3 messages (current, short brief, short brief + prompt-craft
  advice) × 3 contents (opening frame + Vietnamese line, no image + camera
  move in 9:16, character sheet + English line) × 2 runs, plus 2 + 2 long
  videos of 20 s and a final run of the new code (6 jobs). Every one of the 30
  videos had the asked ratio and length; frames, Whisper transcripts and
  loudness showed no difference in quality between the long and short
  messages. Time varied 1-13 min for either length. The only content failure
  was the current message reading a speaker label aloud ("Ông Tư says,
  bà con...").

## Decision
1. **The message is a brief for the agent, the prompt is the scene.** Lines
   before `Prompt:` say only what the agent must do; nothing in them names a
   genre, language, voice or style.
2. One clip: `requestRules` (this message only; ignore earlier chats),
   the image lines, `formatLines`, `planRule` (the prompt is the plan; lines
   word for word, in their language, by their speaker; speaker names are
   labels, not words to say; add nothing unasked), `fitRule` (make nothing and
   say how long is needed when the lines cannot fit). Images get `planRule`
   for images.
3. Images without roles: one image takes the role the prompt gives it, else it
   is the subject (look kept, background/framing/pose from the prompt);
   several are separate elements. Roles and locks (`roleLines`) keep their
   priority list; the long precedence paragraph became one sentence. A video
   with reference images gets one `referenceSheetRule` line.
4. Long videos keep the protocol `parseLongStatus` reads (CHANGED, one STATUS
   line, INCOMPLETE at the per-message limit, TOO SHORT, REFUSED) and the
   workflow of ADR 0013 (plan over clips, shared descriptions and anchors,
   cuts to a new angle, join and check), each as one short line.
   `continueMessage` is one sentence.
5. **Only what Muse does is offered or promised.** The source video is a
   *sample video* Muse remakes as a similar new video (`sampleLines`: follow
   its shots, camera, timing and actions except what the prompt changes;
   speech only from the prompt); nothing calls it v2v. Storyboard images are
   *scene images* used as references for their scenes (`boardImageLines`,
   `boardVideoLines`), never "opening frames". No message or screen says a
   video starts on an attached image. Both were run live with the new wording
   (sample: red-apron remake with the line kept; storyboard: 2 scenes, 20 s,
   STATUS: DONE).
6. Prompt writers (Film's compiler, users) should put scene facts in the
   prompt for this clip only: notes for other scenes (a whole film's colour
   grade), locks on things out of frame, and duplicated "Avoid" lines confuse
   the agent; dialogue written as `Name: "line"` or `Name says: "line"` both
   work, the label rule above covers being read aloud. "Start from the
   attached opening frame" cannot be honoured literally; say what the image
   is a reference for instead.

## Consequences
- Messages are ~3x shorter (one clip with an image: 6,833 → ~3,300 characters
  including a 2,260-character prompt; long 20 s: 10,561 → ~3,900).
- Verification by the agent (listening to lines, measuring the file) is no
  longer asked for in every message; the app measures ratio and length itself
  (`measureNote`). Muse still checks on its own at times.
- The results rest on 30 generations: enough to show the long rules bought
  nothing measurable, not to rank small differences. Re-run
  `TestLiveConsole` with the scripts in its doc comment before growing the
  brief again.
