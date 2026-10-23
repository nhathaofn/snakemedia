# Flow Image: Single Panel Model & Generation Parameters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Single tab inside the Flow Image sidebar with real backend-driven model selection, balanced aspect ratio selector (16:9, 4:3, 1:1, 3:4, 9:16), batch quantity (x1 to x4), and an automatic round-robin account rotation pool in Go that strictly validates real cookies.

**Architecture:** 
- A new `FlowImageService` in `internal/services/flow_image.go` interfaces with `AccountService` to retrieve stored accounts.
- Accounts are filtered for `Status == AccountConnected` and strictly validated for complete, non-expired Google session cookies (`__Secure-1PSID`, `__Secure-3PSID`, `__Secure-1PAPISID`, `__Secure-3PAPISID`, `SID`, `HSID`, `SSID`). Any account with faulty or missing cookies is excluded and logged. If no valid accounts exist, the backend stops immediately with `models.CodeUnauthenticated` / `models.CodeUnavailable` without fake fallbacks.
- An atomic round-robin pool rotates across available connected accounts for requests.
- Real models are fetched using the authenticated session from Google Flow.
- The React frontend displays the 2 tabs ("Single" and "All") inside the 240px sidebar layout, querying `App.GetImageModels()` via `frontend/src/services/api.ts`.

**Tech Stack:** Go 1.26, Wails v2, React 19, TypeScript, Lucide React, CSS Tokens.

---

## Global Constraints
- Zero hardcoding of fake data: if cookies are missing or invalid, fail with an explicit diagnostic message and stop.
- All credential management and rotation logic stays strictly in Go backend; credentials never cross the Wails IPC bridge.
- Frontend only receives public model DTOs and renders them.
- All fast checks in `.agents/verify.json` must pass cleanly.

---

## Tasks

- [ ] **Task 1: Go Backend - FlowImageService, Cookie Validation & Round-Robin Pool**
  - Create `internal/services/flow_image.go` with `FlowImageService`, `FlowModel`, `ImageConfig` structs.
  - Implement cookie validation: check presence and expiry of core Google session cookies.
  - Implement thread-safe round-robin selection among valid connected accounts.
  - Implement `GetImageModels(ctx context.Context) models.Response[[]FlowModel]`.
  - Add unit tests in `internal/services/flow_image_test.go` testing cookie validation, pool filtering, round-robin distribution, and error handling when no accounts/cookies exist.

- [ ] **Task 2: Wails Bridge Binding & TypeScript DTOs**
  - Register `FlowImageService` into `App` in `app.go`.
  - Expose `GetImageModels() models.Response[[]services.FlowModel]` in `app.go`.
  - Expose `GetActiveAccountCount() models.Response[int]` to allow frontend to show how many accounts are currently active in rotation.
  - Update `frontend/src/services/api.ts` and `frontend/src/types/backend.ts` with typed helpers.

- [ ] **Task 3: Frontend - Flow Image Sidebar Panel UI (Single & All Tabs)**
  - Create `FlowImageSidebar.tsx` and `FlowImageSidebar.css` in `frontend/src/features/flow/image/`.
  - Implement tab switcher: `Single` and `All`.
  - Inside `Single` tab:
    - **Model Selection**: Dropdown with loading state, empty/error state when cookies fail, and real model options.
    - **Aspect Ratio Selector**: 5 ratios (`16:9`, `4:3`, `1:1`, `3:4`, `9:16`) in a neat balanced grid with aspect preview icons.
    - **Quantity Selector**: Segmented control for `x1`, `x2`, `x3`, `x4`.
    - **Rotation Info**: Status badge indicating the number of accounts currently rotating.
  - Integrate into `FlowImagePage.tsx`.

- [ ] **Task 4: Verification & Gate Checks**
  - Run `go test ./...` and `go vet ./...`.
  - Run `npm --prefix frontend run test`.
  - Run `npm --prefix frontend run lint` and `format:check`.
  - Run `npm --prefix frontend run build` and `go build ./...`.
