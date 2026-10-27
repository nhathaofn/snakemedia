package engine

import (
	"errors"

	"SnakeMedia/internal/models"
)

// Error is a node failure that keeps the result code the UI shows
// (invalid input, provider unavailable, …).
type Error struct {
	Code    models.Code
	Message string
}

func (e *Error) Error() string { return e.Message }

// Invalid reports a request the node cannot run as given.
func Invalid(message string) error {
	return &Error{Code: models.CodeInvalidArgument, Message: message}
}

// PendingError ends a node whose provider accepted the work but has not
// finished it yet. It is not a success: nodes after it never run on missing
// files. JobIDs name the jobs to resume (never send again), so the work is
// paid for once.
type PendingError struct {
	JobIDs  []string
	Message string
}

func (e *PendingError) Error() string { return e.Message }

// Pending reports work still running on the provider's side.
func Pending(message string, jobIDs []string) error {
	return &PendingError{JobIDs: jobIDs, Message: message}
}

// FromResponse turns a service response into a node result.
func FromResponse[T any](res models.Response[T]) (T, error) {
	if !res.Success {
		var zero T
		return zero, &Error{Code: res.Code, Message: res.Error}
	}
	return res.Data, nil
}

// Respond turns a node result back into the response envelope the app returns.
func Respond[T any](out T, err error) models.Response[T] {
	if err == nil {
		return models.SuccessResponse(out)
	}
	return models.ErrorResponse[T](err.Error(), codeOf(err))
}

func codeOf(err error) models.Code {
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	var pending *PendingError
	if errors.As(err, &pending) {
		return models.CodeUnavailable
	}
	return models.CodeInternal
}
