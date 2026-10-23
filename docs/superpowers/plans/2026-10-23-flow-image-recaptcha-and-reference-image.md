# Flow Image reCAPTCHA Token Minting & Reference Image (Image-to-Image) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable real Google Flow image generation by minting reCAPTCHA Enterprise tokens via persistent background Headless Chrome CDP and supporting Image-to-Image reference uploads via RPC `maseQ`.

**Architecture:** Combine Go's high-speed native HTTP client with Chrome CDP: a persistent Headless Chrome instance evaluates a de-trapped reCAPTCHA mint script in the authenticated Flow tab to obtain a valid `"IMAGE_GENERATION"` token; if an image reference is supplied, it is registered via RPC `maseQ`; finally, the verified 14-slot Protobuf payload is dispatched via RPC `ogiZ0b` and CDN URLs are parsed.

**Tech Stack:** Go 1.26, Wails v2, Chrome DevTools Protocol (CDP over WebSocket), React 19, TypeScript 5, Zustand 5, Lucide React.

**Spec:** [docs/decisions/0002-production-modular-architecture.md](file:///D:/nhathao/reference/SnakeMedia/docs/decisions/0002-production-modular-architecture.md) & [references/flow-research/verified/workflows/image_generation_workflow.md](file:///D:/nhathao/reference/SnakeMedia/references/flow-research/verified/workflows/image_generation_workflow.md).

## Global Constraints

- **Zero hardcode:** Sitekey (`6LdsFiUsAAAAAIjVDZcuLhaHiDn5nnHVXVRQGeMV`) and RPC names (`ogiZ0b`, `maseQ`, `SURFACE_ID=22`) defined as typed constants in Go.
- **Security:** Profile credentials remain encrypted with Windows DPAPI; no raw secrets logged or exposed to the frontend.
- **Resource management:** Background Chrome headless instances must be cleanly terminated upon application shutdown.
- **Gate:** Fast checks in `.agents/verify.json` must pass after every task.

## Review Focus

1. Headless Chrome launch timeout when profile lock is active: Must handle gracefully without hanging or crashing.
2. reCAPTCHA trap bypass: Must restore `Object.assign` in finally block to prevent page corruption.
3. Image reference file size & format: Reject non-image or >10MB files before sending to backend.
4. Token expiration: Mint fresh single-use token right before dispatching generation requests.
5. Error mapping: If reCAPTCHA or backend rejects prompt, map cleanly to user-friendly Vietnamese toast message.

---

### Task 1: Chrome CDP reCAPTCHA Token Minting Module

**Files:**
- Create: `internal/services/chrome/recaptcha.go`
- Test: `internal/services/chrome/recaptcha_test.go`

- [x] Write failing unit test in `internal/services/chrome/recaptcha_test.go` verifying JS expression formatting and response parsing for `MintRecaptchaToken`.
- [x] Implement `MintRecaptchaToken(ctx context.Context, port int, policy config.AccountConfig, action string) (string, error)` in `internal/services/chrome/recaptcha.go`:
  - Locate `flow.google.com` page target from `/json`.
  - Connect to WebSocket CDP.
  - Evaluate the de-trapped reCAPTCHA script (intercept `Object.assign` to restore action, wait for `grecaptcha.enterprise.ready`, call `execute`).
  - Extract and return token string.
- [x] Run `go test ./internal/services/chrome -v -run TestMintRecaptchaToken` to verify test passes.
- [x] Commit: `feat(chrome): implement CDP reCAPTCHA Enterprise token minter with anti-tamper trap bypass`

---

### Task 2: Background Headless Chrome Lifecycle & Account Integration

**Files:**
- Modify: `internal/services/account.go`
- Modify: `internal/services/flow_image.go`
- Modify: `main.go`
- Test: `internal/services/account_test.go`

- [x] Write test verifying `EnsureAccountHeadlessBrowser` retrieves or starts a browser instance for a stored account.
- [x] Add `EnsureAccountHeadlessBrowser(ctx context.Context, acc storedAccount) (*chrome.Browser, error)` to `AccountService`:
  - Reuses active browser if running and responsive.
  - Otherwise starts Chrome with `--headless=new` and `--disable-background-timer-throttling` on an allocated port.
- [x] Register cleanup in `main.go` on application shutdown to close all active browser handles.
- [x] Run `go test ./internal/services -v -run TestEnsureAccountHeadlessBrowser`.
- [x] Commit: `feat(accounts): add persistent headless browser lifecycle for active flow accounts`

---

### Task 3: Reference Image Upload (`maseQ`) & Payload Extension

**Files:**
- Modify: `internal/services/flow_image.go`
- Test: `internal/services/flow_image_test.go`

- [x] Write failing test in `internal/services/flow_image_test.go` verifying `BuildImageBatchRequest` with `refMediaIDs` sets slot 2 (Field 3) properly.
- [x] Write failing test verifying `BuildImageUploadRequest` formats RPC `maseQ` envelope and payload.
- [x] Implement `BuildImageUploadRequest(imageBase64 string, mimeType string, fileName string, projectID string, token string) ([]byte, error)` in `internal/services/flow_image.go`.
- [x] Implement `UploadReferenceImage(ctx context.Context, client *http.Client, acc storedAccount, imageBase64 string, mimeType string, fileName string, token string) (string, error)` in `internal/services/flow_image.go`.
- [x] Update `BuildImageBatchRequest` to accept `refMediaIDs []string`.
- [x] Run `go test ./internal/services -v -run "TestBuildImageBatchRequest|TestBuildImageUploadRequest"`.
- [x] Commit: `feat(flow-image): add RPC maseQ upload request builder and reference media payload support`

---

### Task 4: Complete End-to-End Image Generation Dispatch

**Files:**
- Modify: `internal/services/flow_image.go`
- Test: `internal/services/flow_image_test.go`

- [x] Update `GenerateImageRequest` in `flow_image.go` to include `ReferenceImageBase64 string`, `ReferenceMimeType string`, and `ReferenceFileName string`.
- [x] In `generateImagesFromAccount`:
  - Ensure account's headless browser is active via `AccountService`.
  - Mint fresh token with `action = "IMAGE_GENERATION"`.
  - If reference image is present: mint token for upload, call `UploadReferenceImage`, obtain `mediaId`.
  - Dispatch `BuildImageBatchRequest` with token and `mediaId` to Google Flow via HTTP Client.
  - Parse response with `ParseGeneratedImages` and return items.
- [x] Run `go test ./internal/services/...` to verify all tests pass.
- [x] Commit: `feat(flow-image): connect end-to-end image generation pipeline with live token minting`

---

### Task 5: Frontend Reference Image Dropzone & State Integration

**Files:**
- Modify: `frontend/src/features/flow/image/store/flowImageStore.ts`
- Modify: `frontend/src/features/flow/image/single/SingleModePanel.tsx`
- Modify: `frontend/src/features/flow/image/single/SingleModePanel.css`
- Modify: `frontend/src/features/flow/image/workspace/FlowImageWorkspace.tsx`
- Test: `frontend/src/features/flow/image/store/flowImageStore.test.ts`

- [x] Update `flowImageStore.ts` with `referenceImage: { base64: string; name: string; mimeType: string; previewUrl: string } | null` and setters.
- [x] Add unit tests in `flowImageStore.test.ts` for reference image state actions.
- [x] Add `ReferenceImageDropzone` to `SingleModePanel.tsx`:
  - Drag-and-drop zone with file upload dialog.
  - Thumbnail preview with file name and remove button.
  - Validates size <= 10MB and image MIME types.
- [x] Update `FlowImageWorkspace.tsx` to include `referenceImage` in `api.generateImages` payload.
- [x] Run `npm --prefix frontend run test` and `npm --prefix frontend run build`.
- [x] Commit: `feat(flow-image-ui): add reference image dropzone and state to single mode panel`

---

### Task 6: Verification Gate & Documentation

**Files:**
- Modify: `references/flow-research/verified/workflows/image_generation_workflow.md`
- Modify: `AGENTS.md`
- Create: `docs/decisions/0005-flow-image-recaptcha-and-image-to-image.md`

- [x] Run all verification checks from `.agents/verify.json`:
  - `npm --prefix frontend run build`
  - `npm --prefix frontend run lint`
  - `npm --prefix frontend run format:check`
  - `npm --prefix frontend run test`
  - `go run ./tools/gofmtcheck`
  - `go vet ./...`
  - `go test ./...`
  - `go build ./...`
- [x] Record Architectural Decision Record (ADR) in `docs/decisions/0005-flow-image-recaptcha-and-image-to-image.md`.
- [x] Update `AGENTS.md` with pointers to the new decision.
- [x] Final commit and status report.
