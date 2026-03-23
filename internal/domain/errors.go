package domain

import "errors"

// Sentinel errors for the domain layer.
var (
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrInvalidArgument  = errors.New("invalid argument")
	ErrForbidden        = errors.New("forbidden")
	ErrAlreadyInChannel = errors.New("already in channel")
	ErrNotInChannel     = errors.New("not in channel")
	ErrAlreadyShared    = errors.New("already shared")
	ErrChannelArchived  = errors.New("channel is archived")
	ErrAlreadyReacted   = errors.New("already reacted")
	ErrNoReaction       = errors.New("no reaction")
	ErrNameTaken        = errors.New("name taken")
	ErrAlreadyPinned    = errors.New("already pinned")
	ErrInvalidAuth      = errors.New("invalid auth")
	ErrTokenRevoked     = errors.New("token revoked")
	ErrSessionRevoked   = errors.New("session revoked")
)
