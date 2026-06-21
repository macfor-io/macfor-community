package live

import (
	"io/fs"
	"syscall"
	"time"
)

// fileInfo wraps os.FileInfo to add forensically relevant timestamps.
// On macOS, this includes access time, change time, and birth time.
type fileInfo struct {
	fs.FileInfo

	atime time.Time // Last access time
	ctime time.Time // Inode change time
	btime time.Time // Birth/creation time
}

// newFileInfo creates a fileInfo from an os.FileInfo, extracting
// macOS-specific timestamps from the underlying syscall.Stat_t.
func newFileInfo(info fs.FileInfo) *fileInfo {
	fi := &fileInfo{
		FileInfo: info,
	}

	// Extract timestamps from the underlying stat structure
	if sys := info.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			fi.atime = timespecToTime(stat.Atimespec)
			fi.ctime = timespecToTime(stat.Ctimespec)
			fi.btime = timespecToTime(stat.Birthtimespec)
		}
	}

	return fi
}

// AccessTime returns the last access time of the file.
// Returns zero time if not available.
func (fi *fileInfo) AccessTime() time.Time {
	return fi.atime
}

// ChangeTime returns the inode change time (ctime) of the file.
// This is updated when file metadata changes (permissions, ownership, etc.).
// Returns zero time if not available.
func (fi *fileInfo) ChangeTime() time.Time {
	return fi.ctime
}

// BirthTime returns the creation/birth time of the file.
// This is a macOS-specific timestamp that records when the file was created.
// Returns zero time if not available.
func (fi *fileInfo) BirthTime() time.Time {
	return fi.btime
}

// timespecToTime converts a syscall.Timespec to a time.Time.
func timespecToTime(ts syscall.Timespec) time.Time {
	return time.Unix(ts.Sec, ts.Nsec)
}

// Ensure fileInfo implements macfor.FileInfo.
// This is a compile-time check.
type forensicFileInfo interface {
	fs.FileInfo
	AccessTime() time.Time
	ChangeTime() time.Time
	BirthTime() time.Time
}

var _ forensicFileInfo = (*fileInfo)(nil)
