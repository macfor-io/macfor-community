//go:build !darwin

package atime

import "errors"

// errUnsupported is returned by statVolume on platforms without statfs flags.
var errUnsupported = errors.New("volume probing is not supported on this platform")

// statVolume reports that volume probing is unsupported off macOS. Sources on
// other platforms therefore have no probed volumes.
func statVolume(string) (Volume, error) {
	return Volume{}, errUnsupported
}
