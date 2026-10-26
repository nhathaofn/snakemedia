# Review Muse (Giai đoạn 2–5): lỗi cần sửa

## Trạng thái sửa (26/10)

Đã sửa, mỗi mục có test hồi quy trong `internal/providers/muse/review_fixes_test.go`, `frontend/src/features/muse/shared/museStore.test.ts` và `frontend/src/features/muse/theme.test.ts`:

- **Mục 1 (theme):**
  - Mọi biến CSS đã đổi sang token có trong `tokens.css`; bỏ hết giá trị dự phòng và màu hex/rgba viết cứng (có class mới trong `shared/Muse.css`).
  - Đã thêm `color-scheme` cho cả hai theme. Mọi trang đều import `shared/Muse.css`.
  - Đã định nghĩa `muse-loading` và `muse-tag-*`. `theme.test.ts` chặn biến không tồn tại và màu viết cứng quay lại.
- **Mục 2:**
  - 2.1: chỉ lấy câu trả lời và file sau tin nhắn của job (`HistoryBase`, `SeenPaths`).
  - 2.2: `deadProxySet` copy map trong lúc giữ lock; test chạy sạch với `-race`.
  - 2.3: lệnh gửi không bị cắt ngang khi huỷ, sau đó gọi `chat.cancel`.
  - 2.4: job đã có session dùng ngân sách `MaxRechecks` riêng.
  - 2.5: `RetryProduction` chỉ thử lại từ trạng thái lỗi, sau khi tác vụ con đã kết thúc.
  - 2.6: job con được ghi đầy đủ trong một lần (`submit` + `decorate`), có `ProductionPart` để nhận lại sau crash.
- **Mục 3:**
  - 3.1: `GetText` trả về lỗi; `setupProxy` dừng hẳn khi đọc kho proxy lỗi.
  - 3.2: browser được gắn với route proxy, đổi route thì khởi động lại; browser "adopted" cũng khởi động lại; `SaveProxies` đóng các browser đang rảnh.
  - 3.3: thêm cờ chặn WebRTC.
  - 3.4: danh sách proxy trống mà tài khoản có `ProxyAuto` thì báo lỗi.
  - 3.5: lỗi không còn URL chứa key, nội dung nhà cung cấp bị che key và cắt ngắn, dòng proxy bị che mật khẩu.
- **Mục 4.1:** backend phát `muse:jobs-changed`, frontend tải lại danh sách; `setJob(null)` bị bỏ qua.
- **Sửa kèm vì cùng chỗ code:**
  - 2.7: hàm `resetForQueue` dùng chung.
  - 2.8: tác vụ con bị huỷ, bị xoá hoặc gửi lỗi đều chuyển production sang lỗi.
  - 2.9: storyboard được tới `BoardMax` ảnh.
  - 2.10: một phần, gồm `execute` và `collect` không còn ghi đè trạng thái Đã huỷ.
  - 4.2: `museStore.load` báo lỗi thật.

Chưa làm:
- Phần còn lại của 3.5: Shoplike vẫn gọi qua `http://`. Chưa xác nhận được API của họ có hỗ trợ https; cần thử bằng key thật trước khi đổi.
- Các mục [B]/[C] khác vẫn như danh sách dưới đây.

Trạng thái lúc review: cả 8 lệnh trong `.agents/verify.json` đều đạt. Các lỗi dưới đây test hiện có không bắt được. Mỗi mục ghi `file:dòng` để tra; số dòng có thể lệch nhẹ sau khi sửa.

Mức độ:
- **[A]** mất tiền (gửi Muse 2 lần), lộ IP thật, hoặc làm app crash.
- **[B]** sai tính năng hoặc bị treo.
- **[C]** hardcode, khó bảo trì.

## 1. Theme (lỗi bạn đang thấy)

Nguyên nhân: các trang Muse mới dùng biến CSS **không tồn tại** trong `styles/tokens.css`, kèm màu tối viết cứng làm giá trị dự phòng. Vì vậy ở theme sáng các khung vẫn tối, và ở theme tối màu cũng lệch so với phần còn lại của app.

- Biến không tồn tại, đổi sang token có sẵn:

  | Đang dùng | Đổi thành |
  |---|---|
  | `--bg-surface`, `--color-bg-surface` | `--bg-secondary` |
  | `--bg-surface-elevated` | `--bg-elevated` |
  | `--bg-hover` | `--bg-tertiary` |
  | `--color-bg-base` | `--bg-primary` |
  | `--color-primary` | `--primary` |
  | `--color-text-primary`, `--color-text-secondary` | `--text-primary`, `--text-secondary` |
  | `--color-border-subtle` | `--border-subtle` |
  | `--border-color-hover` | `--border-color` |
  | `--color-warning` | `--warning` |
  | `--font-size-sm`, `--font-size-lg`, `--font-size-2xl` | `--font-size-small`, `--font-size-dialog-title`, `--font-size-page-title` |

  Các file liên quan: `library/`, `proxy/`, `script/`, `studio/`, `settings/*.css`, `create/MuseCreatePage.tsx`, `accounts/MuseAccountsPage.tsx`.
- Bỏ mọi giá trị dự phòng `var(--x, #333)` và mọi màu hex/rgba viết cứng:
  - CSS: proxy 21 chỗ, studio 13, library 10, script 10.
  - TSX: create 7, script 9, accounts 4. Phần lớn nằm trong `style={{…}}`, có khoảng 60 inline style; chuyển chúng thành class CSS.
  - Cần màu nền trạng thái thì dùng `--feedback-*-bg` / `--feedback-*-border` có sẵn. Kích thước dùng `--space-*`.
- `styles/tokens.css`: thêm `color-scheme: dark;` trong `:root` và `color-scheme: light;` trong `[data-theme='light']`. Không có dòng này thì ô `<select>`, checkbox và thanh cuộn vẫn theo màu hệ điều hành.
- Các class `muse-loading`, `muse-tag-success`, `muse-tag-danger`, `muse-tag-warning` đang được dùng nhưng chưa định nghĩa ở đâu.
- Các trang script, studio, library, proxy dùng class trong `shared/Muse.css` nhưng không import file này; chúng chỉ chạy đúng nếu đã mở trang Tạo trước.

Kiểm tra lại: `grep -rhoE "var\(--[a-z0-9-]+" frontend/src/features/muse`, so với danh sách biến trong `tokens.css`, thì không còn biến nào thiếu. Lệnh `grep -rnE "#[0-9a-f]{3,8}\b|rgba?\(" frontend/src/features/muse` trả về rỗng.

## 2. Backend: engine tác vụ / production

1. **[A] Từ cảnh thứ 2 trở đi lấy nhầm video của cảnh 1.** Xem `page.js` hàm `result`, `runner.go collect`, `production.go:~252`.
   - `chat.history` trả về toàn bộ chat. Storyboard gửi mọi cảnh vào cùng một thread, nên `Files[0]` luôn là video cũ nhất.
   - Sửa: lưu số sự kiện assistant (hoặc id sự kiện cuối) **trước khi gửi**, và chỉ lấy file hoặc chữ đến **sau** mốc đó. `classifyReply` cũng chỉ đọc phần mới.
2. **[A] Race trên `p.deadProxies` có thể crash app** (`provider.go:~275`). Map được đọc ngoài `p.mu` trong khi goroutine khác ghi.
   - Sửa: copy map trong lúc giữ lock rồi mới gọi `pickProxy`.
3. **[A] Huỷ tác vụ đúng lúc đang gửi thì Muse vẫn làm và vẫn tính lượt** (`api.go CancelJob`, `runner.go send`).
   - Sửa: nếu đang `Sending`, chờ lệnh gửi trả về rồi `chat.cancel` session vừa nhận.
4. **[A] Tác vụ đã gửi bị đánh Lỗi khi chạm `MaxAttempts`** (`runner.go:~480`), dù Muse có thể đã làm xong. Người dùng bấm Chạy lại sẽ tốn thêm một lượt.
   - Sửa: kiểm tra trường hợp có `SessionID` trước điều kiện `MaxAttempts`, và cho nó một ngân sách "dò lại" riêng.
5. **[A] `RetryProduction` có thể tạo job cảnh thứ hai** (`api.go:~1082`), khi job cũ còn đang chạy.
   - Sửa: chỉ cho thử lại từ trạng thái `error`, và chỉ khi job con đã kết thúc.
6. **[A] App crash giữa lúc `SubmitJobs` và lúc ghi `CurrentJobID` thì gửi 2 lần** (`production.go:~194`, `~287`).
   - Sửa: tạo job hoàn chỉnh (có `Thread`, `PinAccount`, `ProductionID`) trong một lần ghi. Khi khởi động lại, tìm job con theo `ProductionID`.
7. **[B] V2V không bao giờ chạy lại được.** `V2VTurn` không được reset khi requeue hoặc thử lại.
   - Sửa: viết một hàm `resetForQueue(j)` (reset `V2VTurn`, `ThreadError`, `SavedFiles`, `SessionID`, `Sending`, `Files`, …). Dùng nó ở cả 5 chỗ đang copy-paste logic reset.
8. **[B] Production bị treo mãi** (`production.go:~220`) khi job con bị huỷ, bị xoá, xong mà không có file, hoặc `SubmitJobs` lỗi (lỗi đang bị bỏ qua).
   - Sửa: coi mọi kết thúc không thành công, và job không còn tồn tại, là `error`.
9. **[B] Storyboard 5–6 cảnh luôn lỗi.** `BoardMax = 6` nhưng `SubmitJobs` chỉ nhận tối đa `MaxImagesPerJob = 4`.
10. **[B] Huỷ tác vụ rồi nó tự sống lại và kẹt.** Các closure trong `updateJob` ghi đè trạng thái mà không kiểm tra trạng thái hiện tại. `collect` cũng có thể biến Đã huỷ thành Xong.
11. **[B] Ghép video lỗi nhưng vẫn báo Xong** (`production.go:~344`), kết quả chỉ có cảnh 1.
12. **[B] Không bao giờ thêm được nhạc.** Code truyền URL `/flow-media/...` cho ffmpeg; cần đổi sang đường dẫn thật bằng `p.media.Path`.
13. **[B] Hàng đợi đứng im trong lúc ffmpeg chạy**, vì merge và nhạc chạy ngay trong vòng `pump`. Chuyển chúng sang goroutine riêng.
14. **[B] Tác vụ có pin không thật sự bị ghim tài khoản** (`scheduler.go`). Khi tài khoản pin tạm nghỉ, cảnh được giao sang tài khoản khác, làm mất tính đồng nhất với storyboard.
15. **[B] Giãn cách gửi không đúng khi `ThreadsPerAccount > 1`.** Hai job cùng đọc chung `LastSentAt` rồi gửi cùng lúc.
16. **[B] `SubmitProduction` không kiểm tra ratio, duration, board.** `MergeVideos` và `RevealFile` nhận đường dẫn tuỳ ý từ frontend.
17. **[B] Backend phát sự kiện `nil`** (`api.go ClearFinished`, `RemoveJobs`) làm frontend crash. Xem mục 4.1.
18. **[C] `StopAfterFailures` có trong config nhưng không ai dùng.** `ProductionStatusStopped` không bao giờ được gán, và chưa có API dừng production.

## 3. Backend: proxy / studio / ffmpeg

1. **[A] File `proxies.json` hỏng hoặc không giải mã được thì mọi tài khoản chạy bằng IP thật.** `proxyStore.GetText` bỏ qua lỗi và trả chuỗi rỗng. Sửa: trả về lỗi, và `setupProxy` phải dừng hẳn.
2. **[A] Đổi proxy nhưng Chrome đang chạy vẫn dùng mạng cũ (hoặc IP thật)**, trong khi UI báo đang dùng proxy (`browsers.go get` bỏ qua `extraArgs`).
   - Sửa: lưu khoá proxy cùng với browser, khác thì mở lại Chrome. `SaveProxies` phải đóng các browser đang rảnh.
3. **[A] WebRTC làm lộ IP thật.** Thêm cờ Chrome `--force-webrtc-ip-handling-policy=disable_non_proxied_udp`.
4. **[A] Danh sách proxy bị xoá trống thì tài khoản đang có `ProxyAuto` tự chuyển sang chạy trực tiếp.** Chỉ `"none"` mới được phép chạy trực tiếp.
5. **[A] Key của proxy xoay bị lộ** vào thông báo lỗi, `jobs.json` và log, vì URL có `key=` và body thô của nhà cung cấp được đưa thẳng vào thông báo. Shoplike còn gọi qua `http://`. `%v` của `*ParsedProxy` in ra cả mật khẩu.
6. **[B] Proxy xoay bị mất user:pass và kiểu SOCKS5.** Code chỉ lấy `ip:port` và ép thành `http`.
7. **[B] Lỗi Chrome không liên quan đến proxy cũng đánh dấu proxy chết**, làm tài khoản đổi IP giữa phiên.
8. **[B] Bridge:**
   - Chuyển tiếp HTTP thường đang sai: phải dùng `req.WriteProxy`. Sau request đầu, các request keep-alive không còn `Proxy-Authorization`.
   - CONNECT lỗi thì đóng kết nối mà không log, không trả 502.
   - Bridge cũ không bao giờ được đóng, kể cả khi `Provider.Close`. Không có deadline nên dễ rò goroutine.
9. **[B] Studio:**
   - Tag có thể trùng nhau, và không bị kiểm tra theo định dạng `@tag`.
   - `SaveStudio` không validate. `Index` của ảnh luân phiên mà âm sẽ làm panic.
   - Các hàm get-rồi-save không giữ lock nên mất cập nhật.
   - `studio.json` hỏng không được cách ly.
   - Tìm nhân vật theo tên đang so khớp chuỗi con ("Mai" khớp cả "email").
   - Chỉ số ảnh luân phiên không bao giờ tăng.
10. **[C] ffmpeg:**
    - Đường dẫn cấu hình sai thì lặng lẽ dùng ffmpeg trên PATH.
    - Đường dẫn tương đối trong danh sách concat, lỗi `out.Close` và lỗi `MkdirAll` đều bị bỏ qua.
    - Trên Windows nên dùng `CREATE_NO_WINDOW`.

## 4. Frontend

1. **[A] Crash do sự kiện `null`** (`useMuseLive.ts`, `museStore.setJob`). Sửa ở backend: phát danh sách hoặc một sự kiện "jobs-changed" riêng. Frontend cũng nên bỏ qua `null`.
2. **[B] `museStore.load` nuốt mọi lỗi** (`.catch(() => [])`). Backend lỗi thì UI vẫn báo "không có tác vụ". `invoke` không bao giờ reject, nên hãy để `unwrap` ném lỗi ra ngoài và gán `loadError`. Bỏ các `Array.isArray` thừa.
3. **[B] Không `unwrap` kết quả, nên lỗi vẫn báo thành công**:
   - `MuseStudioPage` khi thêm tư liệu (`:141`) và lần tải đầu (`:77`, bị treo ở "Đang tải…").
   - `handleToggleModel` / `handleToggleRotate` (`:205`, `:229`) ném lỗi mà không có try/catch.
   - Trang Kịch bản đọc `res.data` mà không kiểm tra `success`.
4. **[B] Chọn nhiều rồi lọc, các mục không còn hiển thị vẫn bị chạy hoặc xoá** (`MuseCreatePage:66`, `MuseLibraryPage:29`).
5. **[B] Trang Cài đặt Muse gọi `updateConfig` mỗi lần gõ phím.** Xoá trống ô thì gửi `0` và bị từ chối, ô mất focus. Cần lưu nháp, ghi khi rời ô (blur) hoặc debounce.
6. **[B] Thư viện hiển thị `savedFiles` (đường dẫn trên đĩa) như media**, nên ảnh/video không tải được và mỗi kết quả hiện 2 lần.
7. **[B] Gửi nhiều production bằng vòng lặp từng cái.** Nếu lỗi giữa chừng, một phần đã vào hàng đợi; bấm lại sẽ tạo trùng.
8. **[B] Proxy:** "Kiểm tra tất cả" chạy trên danh sách cũ khi lưu thất bại. `MuseAccountsPage:338` hiện proxy kèm mật khẩu dạng chữ thường.
9. **[C] Tự khai báo lại DTO của backend** trong `types/backend.ts:83-173` (`StudioData`, `Production`, …), trái quy ước AGENTS.md; đang phải ép kiểu `as unknown as`.
   - Sửa: re-export `muse.*`. Bind enum `ProductionStatus` và trạng thái proxy bằng `EnumBind` thay cho chuỗi `'done'`, `'live'`.
10. **[C] Hardcode lẽ ra phải lấy từ `GetMuseOptions`:**
    - Tỷ lệ, thời lượng, chế độ storyboard, giới tính người dẫn và giọng mặc định (trang Kịch bản).
    - Vai trò tư liệu (lặp ở 3 chỗ) và giới hạn ảnh 3/20.
    - `'default'` project, danh sách đuôi file, tiền tố proxy xoay.
    - Các số `4000` ms, `8000`, `1.8`.
    - Các khoảng min/max đang chép lại từ phần validate của Go.
11. **[C] Accessibility:** nút chỉ có icon thiếu `aria-label`; một số input, select, textarea không có label; `<span onClick>` dùng như nút.
12. **[C] Bảo trì:**
    - `MuseCreatePage.tsx` (791 dòng) và `MuseScriptPage.tsx` (591 dòng) nên tách thành component nhỏ.
    - Các helper `run` và `report` bị chép ở mỗi trang; gom thành một hook `useMuseAction`.

## 5. Hardcode trong backend (nên đưa vào `config.MuseConfig`)

- Các mốc thời gian:
  - `pumpInterval` 30s, grace khi dò phiên 30s, nhịp dò đăng nhập 3s, timeout đọc quota 1 phút;
  - TTL proxy chết 10 phút (lặp 3 lần);
  - timeout API proxy 8s, kiểm tra proxy 8–10s, dial của bridge 15s, nạp cookie 30s;
  - số luồng kiểm tra proxy 5.
- ffmpeg: âm lượng nhạc 0.15 / 0.8, fade, `crf 22`, `preset fast`, `192k`, thư mục nhạc.
- `MaxImagesPerJob`, `MaxReferences`, `MaxPromptRunes`.
- Thư mục mặc định `Videos/SnakeMedia Muse` lặp ở 3 nơi: gom vào một hàm.
- `Validate` chưa kiểm tra `FFmpegPath` và `OutputDir`.

## 6. Bảo trì

- `api.go` (1120 dòng) đang trộn tài khoản, tác vụ, proxy, studio và production. Tách thành `api_accounts.go`, `api_jobs.go`, `api_proxy.go`, `api_studio.go`, `api_production.go`.
- `pumpProductions` dài khoảng 210 dòng. Tách thành hàm cho từng trạng thái (`startBoard`, `checkBoard`, `launchScene`, `checkScene`, `finalize`) để thấy rõ các chuyển trạng thái còn thiếu (mục 2.5, 2.8).
- Viết test cho các lỗi ở mục 2 (cảnh thứ 2 trong cùng thread, V2V chạy lại, production với job con bị huỷ hoặc xoá) và mục 3.1–3.2 trước khi sửa.

## Thứ tự sửa đề xuất

1. Các lỗi [A] ở mục 2.1–2.6, 3.1–3.5 và 4.1.
2. Theme (mục 1).
3. Các lỗi [B].
4. Hardcode và bảo trì.
