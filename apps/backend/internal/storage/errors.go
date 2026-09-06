package storage

import "errors"

var (
	ErrProviderNotFound       = errors.New("storage provider not found")
	ErrProviderExists         = errors.New("storage provider already registered")
	ErrObjectNotFound         = errors.New("storage object not found")
	ErrObjectExists           = errors.New("storage object already exists")
	ErrQuotaExceeded          = errors.New("storage quota exceeded")
	ErrUnavailable            = errors.New("storage provider unavailable")
	ErrAuthentication         = errors.New("storage provider authentication failed")
	ErrAuthenticationRequired = errors.New("storage provider authentication required")
	ErrRateLimited            = errors.New("storage provider rate limited")
	ErrInvalidRequest         = errors.New("invalid storage request")
)
