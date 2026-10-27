# 0008. Muse Provider: Chat-Client Jobs, a Durable Queue and Many Accounts

- **Status:** Accepted; partly superseded (see note)
- **Date:** 2026-10-26

## Context
Two earlier desktop apps automated muse.ai: HaoHao (Electron, vanilla JS) and
Musee (Electron, Express + puppeteer). Both drive Muse's own React chat client
inside a signed-in muse.ai tab rather than typing into the page. They differ in
what happens around that call:

- HaoHao stores the Muse `sessionId` of every sent job and, after a crash or a
  network error, checks that same chat again instead of sending a new one. It
  rotates jobs over accounts with thread limits and reads the quota through
  Muse's `fetchSubscriptionAction` server action.
- Musee re-sends interrupted jobs (spending another generation), falls back to
  the machine's real IP when proxies fail, and scrapes the quota from the
  Settings dialog. It does classify Muse's replies well (usage limit, refused
  prompt, technical error) and imports cookies in several export formats.

> **Superseded parts.** ADR 0010 replaced the shared launcher
> (`flow/chrome.StartBrowser` → `internal/browser`), the DPAPI Muse account
> store (→ `internal/accounts`) and the `muse:accounts-updated` event
> (→ `accounts:updated`). ADR 0011 replaced the fixed options; ADR 0012 the
> retry details (re-check rules, `SentAt`, the journal on `internal/jobs`).

## Decision
1. **`internal/providers/muse`** is a new provider next to `flow`. It shares
   only the Chrome launcher (`flow/chrome.StartBrowser`, which ties Chrome to
   the app process) and the media store.
2. **Page access:** `page.js` (embedded) is HaoHao's page script. `page.go`
   evaluates it in the account's muse.ai tab over a short CDP connection per
   call; the script keeps open files between calls. Every muse.ai dependency
   lives in `page.js`.
3. **Never pay twice:** a job records `Sending` just before a send and the
   `SessionID` once Muse answers. A sent job is only ever re-checked in its own
   chat on its own account. A send whose answer was lost (or a restart while
   `Sending`) fails with an explanation instead of being re-sent; the user
   retries explicitly.
4. **Retry policy** (HaoHao `failurePatch` plus Musee's reply classes): a
   transport error after sending re-checks the chat; Muse producing no file or
   timing out retries in a new chat within `muse.maxAttempts`; a refused prompt
   fails at once; a usage-limit reply or a signed-out page returns the job to
   the queue without spending an attempt and rests the account.
5. **Scheduling** is a pure `plan` function (HaoHao's scheduler): rotate or
   sequential mode over every usable account (signed in, enabled, routed, not
   resting; no account count to configure), `threadsPerAccount`, quota
   pauses with optional top-up and keep-going, and a random gap between sends
   on one account.
6. **Accounts** are a DPAPI-encrypted store (`internal/secure`, shared with the
   Flow account store). An account is signed in through a visible Chrome, or
   seeded from imported muse.ai cookies (cookies for other domains are
   dropped). Cookies never cross the Wails bridge.
7. Policy values live in `config.MuseConfig`; progress reaches the UI through
   the `muse:job-updated` and `muse:accounts-updated` events.

## Consequences
- LadiPage scraping, Facebook Reels, the local CRM API, license code and the
  keyboard-typing fallback were not carried over.
- Proxies, studio assets, storyboards and FFmpeg merging are later phases.
- Unit tests use a fake `Page` and a fake DevTools server; the optional
  `SNAKEMEDIA_TEST_MUSE=1` test opens muse.ai anonymously in a real Chrome.
