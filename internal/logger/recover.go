package logger

import (
	"fmt"
	"runtime/debug"
)

// Recover, deferred at the top of a goroutine, keeps a panic there from
// ending the app (and every account Chrome tied to it): the panic is logged
// with its stack and turned into an error for onPanic, which settles the
// work the goroutine was doing. It must be deferred directly
// (defer logger.Recover(...)), or recover sees nothing.
func Recover(where string, onPanic ...func(error)) {
	v := recover()
	if v == nil {
		return
	}
	Get().Error("recovered from panic", "where", where, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
	err := fmt.Errorf("Lỗi nội bộ của app (%s): %v. Chi tiết đã ghi vào log.", where, v)
	for _, fn := range onPanic {
		fn(err)
	}
}
