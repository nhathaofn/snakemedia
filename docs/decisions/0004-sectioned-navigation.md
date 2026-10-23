# 0004. Sectioned Navigation: Header Tabs Own the Sidebar

- **Status:** Accepted
- **Date:** 2026-10-23
- **Spec:** —

## Context
The 0003 registry was a flat list of screens shown in one sidebar. The app is splitting into several main areas (Home, Flow, more later), each with its own set of screens. A flat sidebar would grow without bound and mix unrelated tools.

## Decision
1. **Model:** navigation is a list of `AppSection`s (`frontend/src/types/navigation.ts`). A section is one navigation entry with an absolute single-segment path (`/home`) and a list of `FeatureRoute`s with relative single-segment paths (`accounts` → `/home/accounts`).
2. **Ownership:** each section lives in its own folder with a manifest, `features/<section>/section.tsx`, and one sub-folder per feature, `features/<section>/<feature>/`. `routes/registry.ts` only composes the manifests (`APP_SECTIONS`) and derives `HEADER_TABS`, `SIDEBAR_PINNED`, `SECTION_NAVIGATION` and `DEFAULT_PATH`.
3. **Shell:** `layout/Header` renders sections with `placement: 'header'` (default) as labelled tabs centred across the window. Sections with `placement: 'sidebar'` (Settings) are pinned at the bottom of the sidebar, just above the version and collapse toggle, and stay visible in every section. `layout/Sidebar` lists the features of the section that owns the current URL (`routes/useActiveSection`).
4. **Routing:** `/` and unknown URLs redirect to the first feature of the first section; a bare section path redirects to its first visible feature. Section and feature routes are case-sensitive, so a URL either matches exactly or falls back. The URL is the only source of the active section; no store state duplicates it.
5. **Typed URLs:** manifests are declared `as const satisfies AppSection`, and the registry derives `ROUTES` with literal types (`ROUTES.home.accounts: '/home/accounts'`). Code navigates through `ROUTES`, never through path strings.

## Consequences
- New header tab: add `features/<section>/section.tsx` and one entry in `APP_SECTIONS`.
- New screen: add one feature to its section's manifest.
- Renaming or removing a feature turns every stale `ROUTES` reference into a compile error.
- `routes/routes.config.test.ts` checks real URL matching: each feature URL renders its element, bare section paths, `/`, unknown and wrongly cased URLs redirect correctly.
- Feature modules may import `ROUTES` from `routes/registry` but must only read it while rendering or in handlers, because the registry imports them.
- `routes/registry.test.ts` enforces unique ids and paths, path shape, one placement per section, a header tab first and at least one visible feature per section.
- `useActiveSection` lives in `routes/`, not `hooks/`: the registry imports feature modules, which import `hooks/`, so a hook there would form an import cycle.
