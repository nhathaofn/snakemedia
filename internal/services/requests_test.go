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
