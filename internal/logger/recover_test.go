package logger

import (
	"strings"
	"sync"
	"testing"
)

func TestRecoverKeepsGoroutinePanicFromEndingTheApp(t *testing.T) {
	var (
		wg  sync.WaitGroup
		got error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer Recover("test worker", func(err error) { got = err })
		panic("boom")
	}()
	wg.Wait()
	if got == nil || !strings.Contains(got.Error(), "boom") || !strings.Contains(got.Error(), "test worker") {
		t.Fatalf("onPanic error = %v, want it to name the panic and where it happened", got)
	}
}

func TestRecoverWithoutPanicDoesNothing(t *testing.T) {
	called := false
	func() {
		defer Recover("quiet", func(error) { called = true })
	}()
	if called {
		t.Fatal("onPanic ran without a panic")
	}
}
