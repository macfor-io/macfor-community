//go:build linux

package directory

import (
	"io/fs"
	"syscall"
	"time"
)

// fileInfo wraps os.FileInfo to add forensically relevant timestamps.
// On Linux, birth time is not available so BirthTime() returns zero time.
type fileInfo struct {
	fs.FileInfo

	atime time.Time // Last access time.
	ctime time.Time // Inode change time.
}

// newFileInfo creates a fileInfo from an os.FileInfo, extracting
// Linux-specific timestamps from the underlying syscall.Stat_t.
func newFileInfo(info fs.FileInfo) *fileInfo {
	fi := &fileInfo{FileInfo: info}

	if sys := info.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			fi.atime = time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
			fi.ctime = time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
		}
	}

	return fi
}

// AccessTime returns the last access time of the file.
// Returns zero time if not available.
func (fi *fileInfo) AccessTime() time.Time { return fi.atime }

// ChangeTime returns the inode change time (ctime) of the file.
// Returns zero time if not available.
func (fi *fileInfo) ChangeTime() time.Time { return fi.ctime }

// BirthTime returns zero time on Linux as birth time is not available.
func (fi *fileInfo) BirthTime() time.Time { return time.Time{} }
