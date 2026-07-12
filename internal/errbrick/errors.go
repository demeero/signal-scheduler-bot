package errbrick

import (
	"errors"
)

var (
	// ErrInvalidData is a generic error that should be used when the input data is invalid.
	// It should be used with a more specific error message.
	// Example: fmt.Errorf("%w: %s", domain.ErrInvalidData, "invalid print date")
	// So, the error message will be: "invalid data: invalid print date", the error type will be domain.ErrInvalidData
	// and the caller can check for it with errors.Is(err, domain.ErrInvalidData).
	ErrInvalidData = errors.New("invalid data")

	// ErrNotFound is a generic error that should be used when the requested resource is not found.
	// It should be used with a more specific error message.
	// Example: fmt.Errorf("failed to fetch user %d: %w", 123, domain.ErrNotFound)
	// So, the error message will be: "failed to fetch user 123: not found", the error type will be domain.ErrNotFound
	// and the caller can check for it with errors.Is(err, domain.ErrInvalidData).
	ErrNotFound = errors.New("not found")

	// ErrConflict is a generic error that should be used when the requested resource already exists or some another data conflict occurs.
	// It should be used with a more specific error message.
	// Example: fmt.Errorf("%w: %s", domain.ErrConflict, "user already exist")
	// So, the error message will be: "conflict: user already exist", the error type will be domain.ErrConflict
	// and the caller can check for it with errors.Is(err, domain.ErrConflict).
	ErrConflict = errors.New("conflict")

	// ErrForbidden is a generic error that should be used when the subject is not correctly authorized.
	// It should be used with a more specific error message.
	// Example: fmt.Errorf("%w: %s", domain.ErrForbidden, "only admins can modify the data")
	// So, the error message will be: "forbidden: only admins can modify the data", the error type will be domain.ErrForbidden
	// and the caller can check for it with errors.Is(err, domain.ErrForbidden).
	ErrForbidden = errors.New("forbidden")

	// ErrUnauthenticated is a generic error that should be used when the subject is not authenticated.
	// It should be used with a more specific error message.
	// Example: fmt.Errorf("%w: %s", domain.ErrUnauthenticated, "invalid token")
	// So, the error message will be: "unauthenticated: invalid token", the error type will be domain.ErrUnauthenticated
	// and the caller can check for it with errors.Is(err, domain.ErrUnauthenticated).
	ErrUnauthenticated = errors.New("unauthenticated")
)
