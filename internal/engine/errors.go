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
	var coded *Error
	if errors.As(err, &coded) {
		return models.ErrorResponse[T](coded.Message, coded.Code)
	}
	return models.ErrorResponse[T](err.Error(), models.CodeInternal)
}
