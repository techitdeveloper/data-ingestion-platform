package repository

import "errors"

// Sentinel errors — callers use errors.Is() to check for these specifically.
var (
	ErrNotFound      = errors.New("record not found")
	ErrDuplicateFile = errors.New("file already exists for this source and date")
)
