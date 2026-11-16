package service

import (
	"errors"
	"net"
)

// deviceReadError separates device availability from sink or validation failures.
// Only source reads are wrapped; storage timeouts retain fail-fast handling.
type deviceReadError struct{ error }

func (e deviceReadError) Unwrap() error { return e.error }

func transientDeviceRead(err error) bool {
	var readErr deviceReadError
	if !errors.As(err, &readErr) {
		return false
	}
	var networkError net.Error
	if errors.As(readErr.error, &networkError) && networkError.Timeout() {
		return true
	}
	var operationError *net.OpError
	return errors.As(readErr.error, &operationError)
}
