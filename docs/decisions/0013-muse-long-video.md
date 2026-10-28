# 0013. Muse Long Videos: the App's Guide, a Status Line, Continue in the Same Chat

- **Status:** Accepted
- **Date:** 2026-10-28
- **Amends:** ADR 0011 (the video lengths offered), ADR 0012 (how a sent long
  video is re-checked)

## Context
Muse's video tool (`media.generate_video`) makes one clip of about 10 seconds
per call, and Muse allows at most 6 successful video calls per message,
retries included (Muse's own description of its tools, asked live on
2026-10-28). Muse's agent can join clips itself (ffmpeg on its machine), read
the last frame of a clip, keep a snapshot of a clip's context, and voice a
whole script in one pass (`tts`); it has no lip sync. The screens offered 2 to
10 seconds.

Asked for 30 or 60 seconds with only a `Duration` line, Muse returned a file
of the right length made of near copies: every 10-second clip retold the whole
prompt (a soldier coming home six times over), and a news anchor changed face
and clothes between clips. Asked with a scene plan written out per clip
(the HaoHao app's wording), clips followed each other.

## Decision
1. **Lengths.** Video jobs offer 10, 20, 30, 40, 50 and 60 seconds
   (`museDurations`). 10 is one clip. Longer is a *long video*: one prompt,
   one chat, up to 6 clips (one message's worth) joined by Muse into one file.
   `MuseOptions.ClipSeconds` lets the screen say how many clips a length is.
2. **The app's guide** (`longVideoLines`, in English like the rest of the
   message) goes after the ratio and duration lines and before the user's
   prompt, unchanged. It names no genre, language or voice. **The prompt is
   the plan:** Muse makes exactly what it describes, in its order: its
   scenes, shots, timings, camera directions, actions and lines as written,
   adding, dropping, merging, reordering or rewriting nothing, and directs
   only what the prompt leaves open. It lays that content over the clips in
   order (a long scene goes on in the next clip, short ones share a clip), so
   no clip shows again what another shows. Users direct their own videos:
   when Muse wrote its own beats, the weight moved away from the scenes that
   mattered. Spoken lines are said word for word by their speaker; Muse
   checks first that the speech fits at a brisk pace (`speechWordsPerSecond`)
   and otherwise makes nothing and answers `STATUS: TOO SHORT <seconds>`.
   The default pace is brisk: speech from the first second, no pauses between
   lines, action during the lines. The rest, in the order Muse asked for: a
   continuity sheet pasted word for word into every clip; anchor images (the
   attached ones, or generated character and setting images); **cuts between
   clips** to a new camera angle, never from the previous clip's last frame
   or snapshot (that made joins look shifted and stalled), keeping
   positions, props, light and the action's progress continuous; sound (lines
   in the clips' own sound, one narrator voice in one pass, or natural
   sound, as the prompt asks); joining as straight cuts and measuring;
   priorities (the prompt's content, order and lines first); what to do when
   the tool blocks a shot (make the closest accepted version and say what
   changed); the per-message limit. The account's saved rule (every request
   on its own) is not repeated.   A video of one clip (10 seconds, or left to the prompt) gets the rules
   that still apply to one clip (`shortVideoLines`): the prompt is the plan,
   details exactly as given, entrances when the prompt brings them, a brisk
   pace, sound as the prompt asks with lines word for word, no added text or
   logos, and the closest accepted version when the tool refuses. It has no
   clip plan, joining or status line.
3. **A status line.** The reply ends with `CHANGED: …` (only when something
   was changed to get past the video tool) and `STATUS: DONE`,
   `STATUS: INCOMPLETE <clips missing>`, `STATUS: TOO SHORT <seconds>` or
   `STATUS: REFUSED <why>`, read from
   the whole text of the latest answer (`parseLongStatus`, `repliesAfter`;
   `replyText` is shortened and would cut it off).
   - DONE: only the last video in the chat is collected (the chat also holds
     every clip). The CHANGED line is kept as the job's `ChangeNote`.
   - INCOMPLETE: `continueMessage` is sent into the same chat (`Page.SendTo`),
     at most `continueLimit` times (the messages the clip count needs, plus
     two spare); then the job fails.
   - TOO SHORT: a script longer than the length; nothing was made, the job
     fails with the length Muse named.
   - REFUSED: a `policyError`, never retried as is.
   - No status line: collected as usual, unless the file is clearly shorter
     than asked; then the chat is read again later (Muse has ended a turn
     with one clip and sent the joined video afterwards).
4. **Pay once, extended to continues.** Before a continue leaves the app the
   job records `Continues` and `RepliesSeen` (chat events already read) and
   moves `SentAt`. A turn ends only on a reply newer than `RepliesSeen`: right
   after a send the chat still shows the last turn as completed. A continue
   whose answer is lost is only read back; one unanswered within the time
   limit fails the job (`longIncompleteError`) instead of cancelling and
   sending the video in a new chat, which would pay for its clips again. A
   continue Muse refuses never becomes a `rejectedSend` (that hands the job to
   another account). An account out of usage between two clips holds the job
   with its chat until the account's rest ends (`waitAccountError`).
5. **Limits stay named constants**: `museClipSeconds` and
   `museVideosPerMessage` are not exposed by muse.ai and are not user
   settings; they are documented with the date they were read.

## Evidence (live, 2026-10-28, `TestLiveLongVideo`)
Without the guide: soldier 60 s, weather bulletin 60 s, fox 20 s, cooking
30 s: right length, repeated content; the anchor changed between clips.
With the guide (Vietnamese draft): bakery 60 s, mango tree 30 s, weather
bulletin 60 s, fox 20 s: one cast, one setting, one voice, a story moving
forward. Blocked by the video tool: a man fighting a storm at sea (one beat),
artillery firing (every clip); this is why step 9 asks for the closest
accepted version.

A dialogue script (35 lines, 199 words) asked at 60 s with the brisk pace
and the fixed-script rule: every line kept in order, no pause over 0.8 s,
12 minutes. Before those rules Muse shortened and reworded lines and padded
clips with pauses.

## Consequences
- A long video takes one account for minutes (2 to 12 per turn were seen) and
  uses one video call per clip, more when Muse remakes one.
- Content the video tool refuses cannot be forced by wording; the job either
  carries Muse's change note or fails with Muse's reason.
- The guide is the part to tune when Muse changes; `TestLiveLongVideo` sends
  the app's real message (`buildMessage`) for a range of kinds of video.
