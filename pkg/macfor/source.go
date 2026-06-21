package macfor

import (
	"io"
	"io/fs"
	"time"
)

// SourceType identifies the type of forensic source.
type SourceType string

const (
	// SourceTypeLive indicates a live running system.
	SourceTypeLive SourceType = "live"

	// SourceTypeImage indicates a disk image.
	SourceTypeImage SourceType = "image"

	// SourceTypeDirectory indicates a directory tree source (mounted image, snapshot, test fixture).
	SourceTypeDirectory SourceType = "directory"
)

// Source abstracts access to a forensic data source, whether a live system
// or a disk image. This allows the same plugins to work with both source types.
type Source interface {
	// Type returns the source type (live or image).
	Type() SourceType

	// OSVersion returns the macOS version of the source system
	// (e.g., "14.2.1" for Sonoma).
	OSVersion() (string, error)

	// Hostname returns the hostname of the source system.
	Hostname() (string, error)

	// SystemInfo returns comprehensive system information.
	// For live sources, this is collected at runtime from various system APIs.
	// For disk images, this is extracted from system files on the image.
	// The returned SystemInfo includes OS details, hardware info, security state,
	// network identity, and temporal context useful for forensic analysis.
	SystemInfo() (*SystemInfo, error)

	// Open opens the file at the given path for reading.
	// The caller is responsible for closing the returned reader.
	Open(path string) (io.ReadCloser, error)

	// Stat returns file information for the given path.
	Stat(path string) (FileInfo, error)

	// ReadDir reads the directory at the given path and returns
	// a list of directory entries.
	ReadDir(path string) ([]DirEntry, error)

	// Glob returns all files matching the pattern.
	// The pattern syntax is the same as filepath.Glob.
	Glob(pattern string) ([]string, error)

	// Walk walks the file tree rooted at root, calling fn for each file
	// or directory in the tree, including root.
	Walk(root string, fn WalkFunc) error

	// Users returns all human user accounts on the source system.
	// System accounts and service accounts are filtered out.
	Users() ([]User, error)

	// ResolvePath resolves path variables like ~ to actual paths.
	// For example, ~/Documents might become /Users/admin/Documents.
	ResolvePath(path string) string

	// ReadPlist reads and parses a plist file into the provided value.
	// The value should be a pointer to a struct or map.
	ReadPlist(path string, v any) error
}

// User represents a user account on the source system.
type User struct {
	// UID is the numeric user ID.
	UID int

	// GID is the primary group ID.
	GID int

	// Username is the login name.
	Username string

	// Name is the full display name.
	Name string

	// HomeDir is the path to the user's home directory.
	HomeDir string

	// Shell is the user's login shell.
	Shell string
}

// FileInfo describes a file and provides access to its metadata.
// This interface is compatible with fs.FileInfo but adds forensically
// relevant timestamp methods.
type FileInfo interface {
	fs.FileInfo

	// AccessTime returns the last access time of the file.
	// Returns zero time if not available.
	AccessTime() time.Time

	// ChangeTime returns the inode change time (ctime) of the file.
	// Returns zero time if not available.
	ChangeTime() time.Time

	// BirthTime returns the creation/birth time of the file.
	// Returns zero time if not available.
	BirthTime() time.Time
}

// DirEntry represents a directory entry.
// This interface is compatible with fs.DirEntry.
type DirEntry interface {
	fs.DirEntry
}

// WalkFunc is the type of function called by Source.Walk to visit each
// file or directory. It follows the same semantics as filepath.WalkFunc.
//
// The path argument contains the full path to the file or directory.
// The info argument is the FileInfo for the path.
// The err argument reports any error accessing the path.
//
// If there was an error accessing the path, err will describe the problem
// and the function can decide how to handle it. Returning nil will continue
// the walk; returning fs.SkipDir will skip the directory; returning any
// other error will stop the walk.
type WalkFunc func(path string, info FileInfo, err error) error
