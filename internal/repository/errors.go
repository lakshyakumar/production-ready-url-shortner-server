package repository

import "errors"

// Shared errors for all repository implementations
var (
	ErrCircuitOpen = errors.New("database circuit breaker is open")
	ErrNotFound    = errors.New("record not found")
	ErrConflict    = errors.New("record already exists")
)
