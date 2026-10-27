package services

import (
	"context"
	"errors"
	"testing"
)

func TestRequestRegistryCancelsOnlyTheRunningRequest(t *testing.T) {
	r := NewRequestRegistry()
	ctx, done := r.Start(context.Background(), "job-1")
	other, otherDone := r.Start(context.Background(), "job-2")
	defer otherDone()

	if !r.Cancel("job-1") {
		t.Fatal("running request was not found")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("cancelled request kept running")
	}
	if other.Err() != nil {
		t.Fatal("cancelling one request stopped another")
	}
	done()
	if r.Cancel("job-1") {
		t.Fatal("a finished request is still registered")
	}
	if r.Cancel("") {
		t.Fatal("an empty ID must never match")
	}
}

// A second call with the same ID (a resume while the generate still waits)
// does not hide the first from Cancel, and each call's end releases only itself.
func TestRequestRegistryKeepsCallsSharingAnID(t *testing.T) {
	r := NewRequestRegistry()
	first, firstDone := r.Start(context.Background(), "job")
	second, secondDone := r.Start(context.Background(), "job")
	secondDone()
	if second.Err() == nil {
		t.Fatal("finished call kept its context")
	}
	if !r.Cancel("job") || !errors.Is(first.Err(), context.Canceled) {
		t.Fatal("the first call could not be cancelled after the second ended")
	}
	firstDone()
	if r.Cancel("job") {
		t.Fatal("finished calls are still registered")
	}
}

func TestRequestRegistryCancelAll(t *testing.T) {
	r := NewRequestRegistry()
	a, doneA := r.Start(context.Background(), "a")
	b, doneB := r.Start(context.Background(), "b")
	defer doneA()
	defer doneB()
	if n := r.CancelAll(); n != 2 {
		t.Fatalf("CancelAll stopped %d calls, want 2", n)
	}
	if a.Err() == nil || b.Err() == nil {
		t.Fatal("every running call must be cancelled")
	}
}
