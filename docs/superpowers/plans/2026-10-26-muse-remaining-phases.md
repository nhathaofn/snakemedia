# Muse: các giai đoạn còn lại (2–5)

Nhánh: `feature/muse-provider`. Giai đoạn 1 đã xong (xem ADR 0008), **chưa commit**.

## 0. Trạng thái hiện tại: ĐANG SỬA DỞ, CHƯA BUILD ĐƯỢC

Giai đoạn 2 đang làm giữa chừng trong `internal/providers/muse/`:

- Đã xong:
  - `config.MuseConfig` có thêm `AutoSave`, `OutputDir`, `FFmpegPath`, `MaxSourceVideoBytes`, `StopAfterFailures`.
  - `MuseJob` có thêm `Label`, `ProjectID`, `Manifest`, `SourceVideo`, `KeepAudio`, `V2VTurn`, `Thread`, `PinAccount`, `ThreadError`, `ProductionID`, `Held`, `SavedFiles`. Kind mới là `MuseJobV2V`. `jobStore.removeWhere` đã có.
  - `Page.Send` có thêm tham số `thread` (chat cũ). Đã sửa `page.go`, `protocol.go` và fake trong test (`threads`, `attachments`, `threadFails`).
  - `runner.go`: `sendNew` (thử gửi vào chat storyboard, nếu Muse từ chối thì mở chat mới), `send` (dùng chung, có giãn cách gửi), `executeV2V` (2 chặng), `buildMessage` nối `Manifest`, `v2vAnalysisPrompt`, `v2vRemakePrefix`.
- **Còn thiếu nên chưa compile:** chưa viết 3 hàm mà `runner.go` đang gọi:
  1. `(p *Provider) sourceAttachment(job, cfg) (Attachment, error)`: đọc `job.SourceVideo` qua `p.media.Path`, kiểm tra `MaxSourceVideoBytes`, mime lấy từ `media.VideoTypes`. Trả `Attachment{Category: "video"}`. Lỗi thì trả `*inputError`.
  2. `(p *Provider) keepSourceAudio(ctx, job, url, i) (string, error)`: gọi ffmpeg `replaceAudio` (port từ `HaoHao-Project/engine/merge.js`: `extractAudio`, rồi `-map 0:v:0 -map 1:a:0 -c:v copy -c:a aac -shortest`), ghi file kết quả vào media store, trả URL mới.
  3. `(p *Provider) saveOutputs(job, files) []string`: nếu `cfg.AutoSave`, chép file vào `OutputDir` (rỗng thì dùng `~/Videos/SnakeMedia Muse`)/`YYYY-MM-DD`/`<slug từ Label hoặc Prompt>-k.ext`. Không ghi đè (thêm hậu tố -2, -3…). Lỗi thì log và bỏ qua.
- **Cũng phải sửa:**
  - `provider.go` `Start`: job nào còn `Sending == true` thì luôn đánh `failed` (`errSendUnknown`), kể cả khi đã có `SessionID`. Lý do: chặng 2 của V2V gửi trong lúc đã có session. Job `processing` có session nhưng không `Sending` thì để nguyên cho chạy tiếp.
  - Test cũ trong `runner_test.go` vẫn dùng chữ ký mới của fake. Thêm test cho: gửi vào thread; thread bị từ chối thì chuyển chat mới; V2V 2 chặng; khởi động lại khi đang chặng 2.

Chạy `go vet ./internal/providers/muse && go test ./internal/providers/muse` cho tới khi sạch, rồi mới làm tiếp.

## Giai đoạn 2: hàng đợi (phần còn lại)

Backend (`api.go`, `scheduler.go`):
- `plan`: bỏ qua job `Held`; bỏ qua job thuộc dự án đang tạm dừng (truyền vào `pausedProjects map[string]bool`). Với job có `Thread` + `PinAccount`: nếu tài khoản pin còn dùng được thì chỉ giao cho tài khoản đó (port từ `HaoHao-Project/engine/scheduler.js`, đoạn `pin`).
- `SubmitJobs`: thêm `Held bool`, `ProjectID`, `Kind v2v` (bắt buộc có `SourceVideo` và ít nhất 1 ảnh), `KeepAudio`.
- API mới: `RunJobs(ids []string)` (bỏ Held), `HoldQueued()` (tạm dừng: queued → held), `RetryFailed()`, `ClearFinished()` (xoá succeeded), `RemoveJobs(ids)`, `EditAndRetry(id, prompt)`, `OpenOutputFolder()`, `RevealFile(path)` (Windows: `explorer /select,`).
- Nối vào `app.go` và `frontend/src/services/api.ts`; chạy `wails generate module` (sau đó **khôi phục `go.mod`/`go.sum`**, vì Wails CLI v2.16 trên máy tự hạ xuống 2.16).

Frontend:
- Trang mới `features/muse/settings/MuseSettingsPage.tsx`: sửa `config.muse` qua `api.updateConfig`. Gồm headless, số lần thử, timeout ảnh/video, maxAccounts/totalThreads/threadsPerAccount, mode (enum `config.MuseMode`), useTopup, keepGoing, usagePauseMinutes, sendGap, autoSave, outputDir (nút chọn thư mục), ffmpegPath.
- Trang Tạo: nút "Chỉ thêm vào hàng đợi" (Held), checkbox chọn nhiều + Chạy/Xoá đã chọn, "Chạy lại lỗi", "Xoá đã xong", "Tạm dừng hàng đợi", sửa prompt rồi chạy lại, mở thư mục lưu.

## Giai đoạn 3: proxy

Nguồn tham khảo: `HaoHao-Project/engine/proxy.js` (parse, kiểm tra CONNECT tới muse.ai:443, chọn proxy gắn cố định, **không bao giờ dùng IP thật**) và `Musee-v1.0.0-win64/resources/app/dist/server/proxyManager.js` dòng 25–410 và 559–883 (proxy xoay).

- `internal/providers/muse/proxy.go`:
  - `ParseProxy(line)` nhận các dạng:
    - Tĩnh: `ip:port`, `ip:port:user:pass`, `user:pass@ip:port`, `http://[user:pass@]host:port`, `socks5://[user:pass@]host:port`.
    - Xoay: `proxyvn:KEY`, `tmproxy:KEY`, `shoplike:KEY`.
    - URL có path hoặc query thì coi là link API xoay (sửa lỗi của Musee, vốn coi mọi `http://` là API).
  - `checkStatic`: mở CONNECT muse.ai:443. Với socks5 dùng `golang.org/x/net/proxy`.
  - `fetchRotating(provider, key, requestNew)`: port đúng các endpoint và cách đọc JSON của `checkProxyVn` / `checkTMProxy` / `checkShoplike` / custom URL trong Musee. Trạng thái: live / rate_limited / limit_reached / dead.
  - `pickProxy`: port HaoHao. Tài khoản `Proxy` = `""` (tự chọn từ kho) | `"none"` | một dòng cụ thể. Proxy tự chọn được giữ cố định (`ProxyAuto`). Proxy chết thì chọn proxy sống ít tài khoản dùng nhất. Hết proxy sống thì **báo lỗi**.
- `bridge.go`: proxy cục bộ `127.0.0.1:0` cho từng upstream (xử lý CONNECT và HTTP absolute-URI, gắn `Proxy-Authorization` hoặc auth socks5). Chrome luôn chạy với `--proxy-server=http://127.0.0.1:<port>`.
- Thêm `ExtraArgs []string` vào `flow/chrome.LaunchOptions` và `BuildArgs` (không đụng `recaptcha.go`). `browserPool.get` nhận thêm proxy args.
- Kho proxy lưu ở `muse/proxies.json` dạng DPAPI (`internal/secure`), không để trong config.json vì có mật khẩu.
- Tài khoản: thêm `Proxy`, `ProxyAuto`, `ProxyStatus`, `ProxyIP`. API: `GetProxies`/`SaveProxies(text)`/`CheckProxies()`/`SetAccountProxy(id, line)`/`RotateAccountIP(id)`. Đổi proxy thì đóng các trình duyệt đang rảnh.
- Mở trang lỗi do proxy: đánh dấu proxy chết trong 10 phút để lần sau chọn proxy khác.
- Frontend: trang "Proxy" (ô danh sách, kiểm tra, bảng trạng thái), ô chọn proxy trên thẻ tài khoản, nút "Đổi IP".

## Giai đoạn 4: Studio / Tư liệu, @tag, V2V, Thư viện

- `studio.go` (`muse/studio.json`):
  - Dự án (`default` + các dự án khác, có cờ `paused`) và dự án đang mở.
  - Tư liệu = **ảnh gắn tag**: `{tag, name, role: face|full|storyboard|product|other, lockNotes, images ≤3 (local URL)}`. Gộp nhân vật của HaoHao với ref của Musee.
  - Mẫu prompt `{name, text}`.
  - Người mẫu cố định `{on, images ≤3}`; ảnh luân phiên `{on, images ≤20, idx}`.
- Ghép ảnh lúc submit, port `HaoHao-Project/ui/compose.js` `imagesFor`: người mẫu → các @tag xuất hiện trong prompt (theo thứ tự) → 1 ảnh luân phiên → ảnh tham chiếu của lô (chế độ dùng chung hoặc ghép cặp `refs[i]`). Tối đa `MaxReferences` ảnh. Đóng băng vào `job.References`. `job.Manifest` = port `buildRefManifest` trong `Musee/.../refManifest.js`. Thiếu @tag thì báo lỗi ngay, không gửi.
- V2V UI: chọn video nguồn (dùng `api.pickVideo` có sẵn, trả local URL), 2 preset prompt ("Zoom áo", "Thay áo": copy từ `HaoHao-Project/ui/studio.js` dòng ~105–125), tuỳ chọn "Giữ 100% âm thanh gốc".
- Thư viện: trang lọc ảnh/video/dự án từ `jobs[].files`; các nút lưu, mở thư mục, thêm ảnh vào Tư liệu, ghép các video đã chọn.

## Giai đoạn 5: Kịch bản, storyboard, ghép video

- `ffmpeg.go`: tìm ffmpeg theo thứ tự `cfg.FFmpegPath` → `<exe dir>/bin` → PATH; ffprobe nằm cạnh ffmpeg. Port `merge.js`: `merge` (concat `-c copy`, % tiến độ), `addMusic` (bài ngẫu nhiên trong `muse/music`, nhạc ở mức 15% dưới giọng đọc, 80% khi video không có tiếng, fade), `extractAudio`, `replaceAudio`.
- `production.go` (`muse/productions.json`): một production = một video nhiều cảnh.
  - Cấu hình: `prefix, ratio, duration ("10s".."60s" | "each"), voice, host, characterTag, merge, music, board ("" | "auto" | "review")`, `scenes[{text, image}]`, `refs`.
  - Trạng thái: queued → running → review → done | error | stopped.
  - Máy trạng thái chạy trong `pump` (không chặn), dựa trên job con:
    1. Có storyboard: tạo job ảnh (`expect = min(số cảnh, 6)`). Khi xong thì lưu `boardFiles`, `thread = sessionId`, `threadAccount`. Nếu chế độ `review` thì dừng chờ duyệt.
    2. Mỗi nhóm cảnh (mỗi cảnh riêng, hoặc gộp 1 lệnh) tạo 1 job video. Nếu có storyboard thì dùng `Thread` + `PinAccount`, đính kèm lại ảnh storyboard.
    3. Đủ cảnh thì ghép (chế độ từng cảnh) → thêm nhạc → done. Lưu file vào media store và tự lưu ra thư mục.
  - Giới hạn số production chạy cùng lúc = min(totalThreads, maxAccounts, số tài khoản đang bật). Thất bại liên tiếp `StopAfterFailures` lần thì ngừng nhận production mới cho tới khi bấm "Bắt đầu".
  - "Thử lại" giữ lại các cảnh đã xong. "Vẽ lại storyboard" xoá board.
- `script_prompt.go` (hàm thuần, có test): port nguyên `buildPrompt`, `buildBoardPrompt`, `boardVideoPrompt`, `hostLine`, `PACE`, `SELF_VOICE` từ `HaoHao-Project/ui/script.js` dòng 296–624.
  - Danh sách 15 giọng lấy từ `HaoHao-Project/ui/index.html` dòng 265–283. Đưa ra frontend qua `GetMuseOptions`.
  - Soạn lô: các đoạn cách nhau bằng dòng trống = 1 video, mỗi dòng = 1 cảnh. Số video = số kịch bản × số ảnh × số bản (1–20). Chế độ "Từ ảnh" thêm tiền tố "Từ ảnh sản phẩm đính kèm:" và có 4 preset chuyển động (index.html dòng 219–222).
- Frontend: trang "Kịch bản" (soạn, ước tính thời gian `estMin`, hàng đợi production, duyệt storyboard, xem trước).

## Kiểm tra trước khi xong mỗi giai đoạn

- Chạy đủ 8 lệnh trong `.agents/verify.json`.
- Sau `wails generate module`: `git checkout -- go.mod go.sum`.
- Test thật tuỳ chọn: `SNAKEMEDIA_TEST_MUSE=1 go test ./internal/providers/muse -run TestLiveAnonymousPage -v`.
- Cập nhật ADR 0008 (hoặc viết ADR 0009) và AGENTS.md cho mỗi giai đoạn.
