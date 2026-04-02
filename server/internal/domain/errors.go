package domain

import "errors"

// Sentinel errors for the domain layer.
var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadyExists     = errors.New("already exists")
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrForbidden         = errors.New("forbidden")
	ErrUnavailable       = errors.New("unavailable")
	ErrEmailAuthDisabled = errors.New("email auth disabled")
	ErrAlreadyInChannel  = errors.New("already in channel")
	ErrNotInChannel      = errors.New("not in channel")
	ErrAlreadyShared     = errors.New("already shared")
	ErrChannelArchived   = errors.New("channel is archived")
	ErrAlreadyReacted    = errors.New("already reacted")
	ErrNoReaction        = errors.New("no reaction")
	ErrNameTaken         = errors.New("name taken")
	ErrInvalidAuth       = errors.New("invalid auth")
	ErrTokenRevoked      = errors.New("token revoked")
	ErrSessionRevoked    = errors.New("session revoked")
)
