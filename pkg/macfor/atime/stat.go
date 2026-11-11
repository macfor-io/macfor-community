package atime

import (
	"io/fs"
	"time"
)

// Stat is a snapshot of a file's inode metadata, taken before or after a read.
type Stat struct {
	Dev       uint64    `json:"dev"`
	Ino       uint64    `json:"ino"`
	Mode      string    `json:"mode"`
	Nlink     uint64    `json:"nlink"`
	UID       uint32    `json:"uid"`
	GID       uint32    `json:"gid"`
	Size      int64     `json:"size"`
	Atime     time.Time `json:"atime"`
	Mtime     time.Time `json:"mtime"`
	Ctime     time.Time `json:"ctime"`
	Birthtime time.Time `json:"birthtime,omitzero"`
}

// StatOf snapshots fi. It reports false when fi carries no platform stat
// structure, in which case the read cannot be observed.
func StatOf(fi fs.FileInfo) (Stat, bool) {
	if fi == nil {
		return Stat{}, false
	}
	s, ok := statFromSys(fi.Sys())
	if !ok {
		return Stat{}, false
	}
	s.Mode = fi.Mode().String()
	s.Size = fi.Size()
	return s, true
}

// sameInode reports whether a and b describe the same inode.
func sameInode(a, b Stat) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino
}

// Attribution states what most likely changed an access time. A change can
// never be proven to come from the collector: another process may have read
// the file during the same window.
type Attribution string

const (
	// AttributionLikelyCollector means the access time moved forward while
	// the content, size and inode change time stayed the same.
	AttributionLikelyCollector Attribution = "likely_collector"

	// AttributionConcurrentActivity means the modification time, inode change
	// time or size also changed during the read, or the access time moved
	// backwards. Another process was active on the file, and a copy taken
	// during the read may be inconsistent.
	AttributionConcurrentActivity Attribution = "concurrent_activity"
)

// compare reports whether the access time changed between before and after,
// and how the change is attributed.
func compare(before, after Stat) (changed bool, attribution Attribution) {
	if before.Atime.Equal(after.Atime) {
		return false, ""
	}
	if !before.Mtime.Equal(after.Mtime) || !before.Ctime.Equal(after.Ctime) ||
		before.Size != after.Size || after.Atime.Before(before.Atime) {
		return true, AttributionConcurrentActivity
	}
	return true, AttributionLikelyCollector
}
