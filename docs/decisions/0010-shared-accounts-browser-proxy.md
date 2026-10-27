# 0010. One Account Service for Every Provider: Shared Browser, CDP and Proxy

- **Status:** Accepted
- **Date:** 2026-10-26
- **Supersedes:** the account and proxy parts of ADR 0008 (§6) and ADR 0009 (§2)

## Context
Flow and Muse each grew their own account machinery:

- Flow: `flow/session` (store, login popup polled by the frontend, browser
  map), Chrome launch and CDP helpers inside `flow/chrome`, accounts under
  Home → Tài khoản, no proxy, no cookie import.
- Muse: its own store, `browserPool`, CDP connection, cookie import, proxy
  list, bridge and picker, accounts and proxy pages in the Muse sidebar.

The same feature existed twice with different behaviour (Flow had no proxy,
Muse could not share Flow's lifecycle fixes), the screens were split across
sections, and Muse's settings were one long flat list.

## Decision
1. **`internal/browser`** owns Chrome for everyone: launcher (job object,
   OS-assigned debug port), `Pool` (one browser per profile, restart on a
   changed proxy route, never restart a browser that `Acquire`d work uses),
   CDP calls (`Call`, `Evaluate`, `Conn`), tabs (`EnsureTab`, a site's boot
   script) and cookie parsing/injection. `flow/chrome` keeps only Flow's
   protocol and calls these helpers.
2. **`internal/proxy`** owns the proxy list (encrypted), parsing, health checks,
   rotating providers, the local bridge and `Manager.Route`, which picks an
   account's proxy (explicit, `none`, or the least shared live list line,
   pinned) and fails rather than fall back to the real IP.
3. **`internal/accounts`** owns every provider's accounts: one encrypted store
   (`accounts/accounts.dat`), profiles under `accounts/profiles`, sign-in
   through a watched window or imported cookies, checks, enable/disable,
   rename, proxy assignment, IP rotation and removal. A provider contributes a
   `Site` (home page, which URLs it owns, cookie domains, `Inspect`) and keeps
   its own runtime data in `Record.Data` (Flow: session values and balance;
   Muse: quota and last send). Changes reach the UI as `accounts:updated`.
4. Every account browser starts on its proxy route; a provider's own HTTP
   clients use the same route through `ProxyURL` (Flow's `FlowClient.SetProxy`).
   Flow's balance is read through the page over CDP, like Muse's quota.
5. **UI:** Home has Tài khoản (one page, a tab per provider) and Proxy.
   Provider sidebars hold only their work and their settings. Settings screens
   share `components/settings`: a few main rows per group, the rest under
   "Nâng cao"; shared browser policy lives in Cài đặt → Chung.
6. Existing account stores are not migrated: the new store starts empty and
   users sign in again. Sent Muse jobs whose account no longer exists fail on
   start instead of being sent again.

## Consequences
- A new provider adds a `Site` and its work; accounts, browsers, cookies and
  proxies come with it.
- `config.AccountConfig` gains `headless` and `pageReadySeconds` (from Muse);
  `cdpPort` and Muse's login/headless settings are gone.
- Flow-only generation limits stay in `config.AccountConfig` for now but are
  edited on the Flow settings screen.
