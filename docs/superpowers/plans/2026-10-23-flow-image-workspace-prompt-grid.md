# Flow Image Workspace: Prompt Bar, Generate Trigger & 4-Column Grid Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the right content area of Flow Image with a pinned bottom prompt bar, generate button, 4-column responsive image grid with hover actions (preview/download/copy prompt), shared Zustand store with SingleModePanel, and Go backend GenerateImages API with round-robin rotation.

**Architecture:**
- **State Store (`useFlowImageStore`)**: Centralizes `model`, `aspectRatio`, `quantity`, `prompt`, `isGenerating`, `images` in Zustand so the left sidebar parameters and right generation workspace stay perfectly synchronized.
- **Backend (`FlowImageService.GenerateImages`)**: Validates parameters, selects the next account from the thread-safe round-robin pool via `NextAccount()`, validates cookies, dispatches image generation, and returns standardized image items (`models.Response[[]GeneratedImageItem]`).
- **Frontend Workspace**:
  - Main scrollable area: 4-column grid (`display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px;`) rendering image cards with aspect ratios, hover overlay controls, and loading skeleton shimmer cards.
  - Pinned bottom bar: Multiline auto-expanding prompt input (`Ctrl+Enter` trigger), parameter badges, and primary Generate button with loading spinner.
  - Image preview modal for inspection.

**Tech Stack:** Go 1.26, Wails v2, React 19, TypeScript, Zustand 5, Lucide React, CSS Tokens.

---

## Global Constraints
- Grid must strictly be 4 images per row as specified by the user.
- Prompt bar and Generate button must strictly be pinned at the bottom of the right layout.
- Strictly no fake hardcoded images in production flow; when generating fails or cookies are invalid, show clear error state.
- All checks in `.agents/verify.json` must pass cleanly.

---

## Tasks

- [ ] **Task 1: Shared Zustand State Store (`useFlowImageStore`)**
  - Create `frontend/src/features/flow/image/store/flowImageStore.ts`.
  - Connect `SingleModePanel.tsx` to read and update `model`, `aspectRatio`, `quantity` through the store.
  - Add tests for `flowImageStore.ts`.

- [ ] **Task 2: Go Backend - GenerateImages API & Round-Robin Dispatch**
  - Add `GenerateImageRequest` and `GeneratedImageItem` in `internal/services/flow_image.go`.
  - Implement `GenerateImages(ctx context.Context, req GenerateImageRequest) models.Response[[]GeneratedImageItem]`.
  - Register `GenerateImages` in `App` in `app.go`.
  - Add unit tests in `internal/services/flow_image_test.go`.
  - Regenerate Wails bindings with `wails generate module`.
  - Update `frontend/src/services/api.ts` and `frontend/src/types/backend.ts`.

- [ ] **Task 3: Flow Image Workspace UI (4-Column Grid, Bottom Prompt Bar & Preview Modal)**
  - Create `FlowImageWorkspace.tsx` and `FlowImageWorkspace.css` in `frontend/src/features/flow/image/workspace/`.
  - Implement 4-column grid with image cards, hover actions (Zoom, Copy Prompt, Download).
  - Implement shimmer loading skeletons for in-flight generation.
  - Implement pinned bottom prompt bar with textarea and Generate button.
  - Implement Image Lightbox Modal for full-resolution inspection.
  - Mount `FlowImageWorkspace` in `FlowImagePage.tsx`.

- [ ] **Task 4: Verification & Gate Checks**
  - Run all Go and frontend unit tests.
  - Run `npm --prefix frontend run lint` and `format:check`.
  - Run `npm --prefix frontend run build` and `go build ./...`.
