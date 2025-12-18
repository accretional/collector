package db

import "fmt"

type CapabilityError struct {
	Feature string
	Message string
}

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("capability '%s' not available: %s", e.Feature, e.Message)
}

func IsCapabilityError(err error) bool {
	_, ok := err.(*CapabilityError)
	return ok
}

type ExtensionError struct {
	Extension string
	Cause     error
}

func (e *ExtensionError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("failed to load extension %s: %v", e.Extension, e.Cause)
	}
	return fmt.Sprintf("failed to load extension %s", e.Extension)
}

func (e *ExtensionError) Unwrap() error {
	return e.Cause
}

func IsExtensionError(err error) bool {
	_, ok := err.(*ExtensionError)
	return ok
}
