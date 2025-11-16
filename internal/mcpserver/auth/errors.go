package auth

import "fmt"

// ErrTokenAcquisitionFailed indicates token acquisition failed
type ErrTokenAcquisitionFailed struct {
	Audience    string
	Interactive bool
	Reason      string
}

func (e ErrTokenAcquisitionFailed) Error() string {
	mode := "silent"
	if e.Interactive {
		mode = "interactive"
	}
	if e.Reason != "" {
		return fmt.Sprintf("failed to acquire token for audience %q in %s mode: %s", e.Audience, mode, e.Reason)
	}
	return fmt.Sprintf("failed to acquire token for audience %q in %s mode", e.Audience, mode)
}

// ErrInvalidConfig indicates Auth0 configuration is invalid
type ErrInvalidConfig struct {
	Field  string
	Reason string
}

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("invalid auth0 config field %q: %s", e.Field, e.Reason)
}

// ErrUserDenied indicates the user denied the authorization request
type ErrUserDenied struct {
	Description string
}

func (e ErrUserDenied) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("user denied authorization: %s", e.Description)
	}
	return "user denied authorization"
}

// ErrTimeout indicates the device code flow timed out waiting for user
type ErrTimeout struct {
	Duration string
}

func (e ErrTimeout) Error() string {
	return fmt.Sprintf("device code flow timed out after %s waiting for user authorization", e.Duration)
}
