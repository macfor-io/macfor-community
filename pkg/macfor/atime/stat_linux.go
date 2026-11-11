//go:build linux

package atime

import (
	"syscall"
	"time"
)

// statFromSys extracts the inode fields from a linux syscall.Stat_t. Linux
// has no birth time in stat(2), so Birthtime stays zero.
func statFromSys(sys any) (Stat, bool) {
	st, ok := sys.(*syscall.Stat_t)
	if !ok || st == nil {
		return Stat{}, false
	}
	return Stat{
		Dev:   st.Dev,
		Ino:   st.Ino,
		Nlink: uint64(st.Nlink), //nolint:unconvert // Nlink is uint32 on some linux architectures.
		UID:   st.Uid,
		GID:   st.Gid,
		Atime: time.Unix(int64(st.Atim.Sec), int64(st.Atim.Nsec)), //nolint:unconvert // Timespec fields are int32 on 32-bit linux.
		Mtime: time.Unix(int64(st.Mtim.Sec), int64(st.Mtim.Nsec)), //nolint:unconvert // See Atime.
		Ctime: time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec)), //nolint:unconvert // See Atime.
	}, true
}
