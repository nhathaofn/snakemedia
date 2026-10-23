package models

// Code classifies the outcome of a backend call. It is bound to the frontend as a
// TypeScript enum (models.Code); values intentionally mirror HTTP status semantics.
type Code int

const (
	CodeOK              Code = 200
	CodeInvalidArgument Code = 400
	CodeNotFound        Code = 404
	CodeInternal        Code = 500
	CodeUnavailable     Code = 503
)

// AllCodes is bound to the frontend as a TypeScript enum (models.Code).
var AllCodes = []struct {
	Value  Code
	TSName string
}{
	{CodeOK, "OK"},
	{CodeInvalidArgument, "InvalidArgument"},
	{CodeNotFound, "NotFound"},
	{CodeInternal, "Internal"},
	{CodeUnavailable, "Unavailable"},
}

// Response represents a standardized API result envelope for frontend communication.
type Response[T any] struct {
	Success bool   `json:"success"`
	Data    T      `json:"data"`
	Error   string `json:"error,omitempty"`
	Code    Code   `json:"code"`
}

// SuccessResponse creates a successful response envelope.
func SuccessResponse[T any](data T) Response[T] {
	return Response[T]{
		Success: true,
		Data:    data,
		Code:    CodeOK,
	}
}

// ErrorResponse creates an error response envelope.
func ErrorResponse[T any](msg string, code Code) Response[T] {
	return Response[T]{
		Success: false,
		Error:   msg,
		Code:    code,
	}
}
