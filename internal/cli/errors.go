package cli

import "errors"

type ExitError struct {
	Code    int
	Message string
}

func (e ExitError) Error() string {
	return e.Message
}

func AsExitError(err error, target *ExitError) bool {
	return errors.As(err, target)
}

func Exit(code int, format string, args ...any) ExitError {
	return exit(code, format, args...)
}

func exit(code int, format string, args ...any) ExitError {
	return ExitError{Code: code, Message: sprintf(format, args...)}
}
