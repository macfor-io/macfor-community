//go:build darwin

package atime

import (
	"syscall"
	"time"
)

// statFromSys extracts the inode fields from a darwin syscall.Stat_t.
func statFromSys(sys any) (Stat, bool) {
	st, ok := sys.(*syscall.Stat_t)
	if !ok || st == nil {
		return Stat{}, false
	}
	return Stat{
		Dev:       uint64(uint32(st.Dev)), //nolint:gosec // dev_t is a 32-bit identifier; reinterpret, never compare as signed.
		Ino:       st.Ino,
		Nlink:     uint64(st.Nlink),
		UID:       st.Uid,
		GID:       st.Gid,
		Atime:     time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec),
		Mtime:     time.Unix(st.Mtimespec.Sec, st.Mtimespec.Nsec),
		Ctime:     time.Unix(st.Ctimespec.Sec, st.Ctimespec.Nsec),
		Birthtime: time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec),
	}, true
}
