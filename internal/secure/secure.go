package secure

import "errors"

// ErrOtherUser means stored data could not be decrypted for the current
// Windows user: it was made by another user or machine (a copied data
// folder), the user's Windows password was reset by an administrator, or the
// file is damaged. DPAPI cannot tell these apart, so the message names all of
// them; the data cannot be moved to another user either way.
var ErrOtherUser = errors.New("không giải mã được dữ liệu cho người dùng Windows hiện tại")

// ForeignDataHelp explains ErrOtherUser to the user.
const ForeignDataHelp = "Dữ liệu này được mã hoá cho một người dùng Windows khác: thư mục dữ liệu được chép từ máy hoặc tài khoản Windows khác, hoặc mật khẩu Windows đã bị quản trị viên đặt lại (cũng có thể file bị hỏng). Dữ liệu mã hoá không chuyển được giữa máy hay người dùng: hãy nhập lại trên máy này."
