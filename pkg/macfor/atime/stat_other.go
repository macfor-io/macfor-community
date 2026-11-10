//go:build !darwin && !linux

package atime

// statFromSys reports that inode metadata is unavailable on this platform,
// so reads are not observed.
func statFromSys(any) (Stat, bool) {
	return Stat{}, false
}
