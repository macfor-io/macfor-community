//go:build darwin

package directory

import (
	"io/fs"
	"syscall"
	"time"
)

// fileInfo wraps os.FileInfo to add forensically relevant timestamps.
// On macOS, this includes access time, change time, and birth time.
type fileInfo struct {
	fs.FileInfo

	atime time.Time // Last access time.
	ctime time.Time // Inode change time.
	btime time.Time // Birth/creation time.
}

// newFileInfo creates a fileInfo from an os.FileInfo, extracting
// macOS-specific timestamps from the underlying syscall.Stat_t.
func newFileInfo(info fs.FileInfo) *fileInfo {
	fi := &fileInfo{FileInfo: info}

	if sys := info.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			fi.atime = time.Unix(stat.Atimespec.Sec, stat.Atimespec.Nsec)
			fi.ctime = time.Unix(stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
			fi.btime = time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec)
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

// BirthTime returns the creation/birth time of the file.
// Returns zero time if not available.
func (fi *fileInfo) BirthTime() time.Time { return fi.btime }
