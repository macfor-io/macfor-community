//go:build !darwin && !linux

package directory

import (
	"io/fs"
	"time"
)

// fileInfo wraps os.FileInfo to add forensically relevant timestamps.
// On unsupported platforms, all extended timestamps return zero time.
type fileInfo struct {
	fs.FileInfo
}

// newFileInfo creates a fileInfo from an os.FileInfo.
// On unsupported platforms, no extended timestamps are extracted.
func newFileInfo(info fs.FileInfo) *fileInfo {
	return &fileInfo{FileInfo: info}
}

// AccessTime returns zero time on unsupported platforms.
func (fi *fileInfo) AccessTime() time.Time { return time.Time{} }

// ChangeTime returns zero time on unsupported platforms.
func (fi *fileInfo) ChangeTime() time.Time { return time.Time{} }

// BirthTime returns zero time on unsupported platforms.
func (fi *fileInfo) BirthTime() time.Time { return time.Time{} }
