package cli

import "errors"

// Process exit codes returned by ExitCode.
const (
	// ExitOK means the command completed without errors.
	ExitOK = 0

	// ExitFailure means the command failed.
	ExitFailure = 1

	// ExitPartial means collection completed and the evidence container was
	// written, but macfor recovered from one or more artifact errors. The
	// errors are recorded in the container's manifest, chain of custody, and
	// plugin logs.
	ExitPartial = 2

	// ExitLicense means the license check refused to start a collection,
	// for example because the license has expired.
	ExitLicense = 3
)

// ErrPartialCollection is returned by the collect command when collection
// completed with recovered errors.
var ErrPartialCollection = errors.New("collection completed with errors")

// ErrLicense is returned by the collect command when the license check
// refuses to start a collection.
var ErrLicense = errors.New("license check failed")

// ExitCode maps the error returned by Execute to a process exit code.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrPartialCollection):
		return ExitPartial
	case errors.Is(err, ErrLicense):
		return ExitLicense
	default:
		return ExitFailure
	}
}
