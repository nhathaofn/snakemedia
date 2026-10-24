# 0006. Providers, Engine Nodes and a Shared Media Store

- **Status:** Accepted
- **Date:** 2026-10-23

## Context
Image and video generation grew inside one `services` package next to account
handling, browser automation and file storage. The next step is a ComfyUI-style
node editor, where each capability is a node with typed inputs and outputs. That
needs clear seams: who talks to Google Flow, who owns accounts, who stores files,
and a uniform way to describe and run a unit of work.

## Decision
1. **`internal/providers/flow`** is the single Google Flow provider: account
   rotation, project and model catalogue, image and video generation. It depends
   on accounts only through the `flow.Accounts` interface.
   - `flow/session` owns Google account sessions: Chrome profiles, the DPAPI
     store and syncing the session from the running profile.
   - `flow/chrome` is the browser/CDP layer, moved without content changes so the
     reCAPTCHA code stays byte-identical.
2. **`internal/media`** stores downloaded images and videos, serves them under
   `/flow-media/` (asset-server middleware) and copies them to a chosen folder.
3. **`internal/engine`** describes work as nodes (`Spec`: typed ports and
   settings) in a `Registry`. Providers implement `providers.Provider` and
   contribute nodes: `flow.image.generate`, `flow.video.generate`; the media store
   adds `media.save`. Settings that depend on the account reference their source
   (`OptionsFrom`) instead of copying values into the spec.
4. The studio pages keep calling the provider's typed methods; nodes wrap those
   same methods, so behaviour does not change. `App.ListNodes` exposes the specs
   for the future node editor.

## Consequences
- A new provider is a new package under `internal/providers/` that registers its
  nodes; nothing else changes.
- Flow unit tests use an in-memory `Accounts` fake and never reach the network.
- A graph runner can be added to `internal/engine` later without touching
  providers.
