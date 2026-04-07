package search

import "errors"

var ErrNotConfigured = errors.New("search is not configured")

type ErrorKind string

const (
	ErrorKindMalformed   ErrorKind = "malformed"
	ErrorKindValidation  ErrorKind = "validation"
	ErrorKindForbidden   ErrorKind = "forbidden"
	ErrorKindNotFound    ErrorKind = "not_found"
	ErrorKindUnavailable ErrorKind = "unavailable"
)

type Error struct {
	Kind    ErrorKind
	Field   string
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func malformed(message string) *Error {
	return &Error{Kind: ErrorKindMalformed, Code: "malformed", Message: message}
}

func invalid(field string, code string, message string) *Error {
	return &Error{Kind: ErrorKindValidation, Field: field, Code: code, Message: message}
}

func forbidden(message string) *Error {
	return &Error{Kind: ErrorKindForbidden, Code: "forbidden", Message: message}
}

func notFound(message string) *Error {
	return &Error{Kind: ErrorKindNotFound, Code: "not_found", Message: message}
}

func unavailable(message string) *Error {
	return &Error{Kind: ErrorKindUnavailable, Code: "unavailable", Message: message}
}
