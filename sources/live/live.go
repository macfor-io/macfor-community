// Package live provides a Source implementation for live macOS systems.
package live

import (
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Source provides access to a live macOS system's filesystem.
// It implements the macfor.Source interface for collecting forensic
// artifacts from the currently running system.
type Source struct {
	// currentUser caches the current user for path resolution.
	currentUser *user.User
}

// New creates a new Source for accessing the live system.
func New() (*Source, error) {
	u, err := user.Current()
	if err != nil {
		return nil, macfor.WrapIOError("lookup", "current user", err)
	}

	return &Source{
		currentUser: u,
	}, nil
}

// Type returns SourceTypeLive indicating this is a live system source.
func (s *Source) Type() macfor.SourceType {
	return macfor.SourceTypeLive
}

// Open opens the file at the given path for reading.
// The caller is responsible for closing the returned reader.
func (s *Source) Open(path string) (io.ReadCloser, error) {
	resolved := s.ResolvePath(path)
	f, err := os.Open(resolved) //nolint:gosec // G304: Intentional file access for forensic collection
	if err != nil {
		return nil, wrapOSError("open", resolved, err)
	}
	return f, nil
}

// Stat returns file information for the given path.
// The returned FileInfo includes forensically relevant timestamps
// (access time, change time, birth time) when available on macOS.
func (s *Source) Stat(path string) (macfor.FileInfo, error) {
	resolved := s.ResolvePath(path)
	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, wrapOSError("stat", resolved, err)
	}
	return newFileInfo(info), nil
}

// ReadDir reads the directory at the given path and returns
// a list of directory entries.
func (s *Source) ReadDir(path string) ([]macfor.DirEntry, error) {
	resolved := s.ResolvePath(path)
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, wrapOSError("readdir", resolved, err)
	}

	result := make([]macfor.DirEntry, len(entries))
	for i, e := range entries {
		result[i] = e
	}
	return result, nil
}

// Glob returns all files matching the pattern.
// The pattern syntax is the same as filepath.Glob.
func (s *Source) Glob(pattern string) ([]string, error) {
	resolved := s.ResolvePath(pattern)
	matches, err := filepath.Glob(resolved)
	if err != nil {
		return nil, macfor.WrapIOError("glob", pattern, err)
	}
	return matches, nil
}

// Walk walks the file tree rooted at root, calling fn for each file
// or directory in the tree, including root.
func (s *Source) Walk(root string, fn macfor.WalkFunc) error {
	resolved := s.ResolvePath(root)

	return filepath.WalkDir(resolved, func(path string, _ fs.DirEntry, err error) error {
		var info macfor.FileInfo

		if err == nil {
			// Get full file info including forensic timestamps
			osInfo, statErr := os.Lstat(path)
			if statErr != nil {
				err = wrapOSError("stat", path, statErr)
			} else {
				info = newFileInfo(osInfo)
			}
		} else {
			err = wrapOSError("walk", path, err)
		}

		return fn(path, info, err)
	})
}

// ResolvePath resolves path variables to actual paths.
// Supports:
//   - ~ expands to the current user's home directory
//   - ~username expands to the specified user's home directory
func (s *Source) ResolvePath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	// Handle ~/... (current user)
	if path == "~" || strings.HasPrefix(path, "~/") {
		return filepath.Join(s.currentUser.HomeDir, strings.TrimPrefix(path, "~"))
	}

	// Handle ~username/... (specific user)
	rest := strings.TrimPrefix(path, "~")
	var username, suffix string
	if idx := strings.Index(rest, "/"); idx != -1 {
		username = rest[:idx]
		suffix = rest[idx:]
	} else {
		username = rest
		suffix = ""
	}

	u, err := user.Lookup(username)
	if err != nil {
		// If user lookup fails, return path unchanged
		return path
	}

	return filepath.Join(u.HomeDir, suffix)
}

// wrapOSError wraps an OS error with the appropriate macfor error type.
func wrapOSError(op, path string, err error) error {
	if os.IsNotExist(err) {
		return macfor.WrapNotFound(path, err)
	}
	if os.IsPermission(err) {
		return macfor.WrapPermissionDenied(path, err)
	}
	return macfor.WrapIOError(op, path, err)
}

// Ensure Source fully implements macfor.Source interface.
// This is a compile-time check that verifies all required methods are implemented.
// The interface implementation is split across:
//   - live.go: MACFOR-2.1 (Type, Open, Stat, ReadDir, Glob, Walk, ResolvePath)
//   - system.go: MACFOR-2.2 (OSVersion, Hostname, ReadPlist)
//   - users.go: MACFOR-2.3 (Users)
var _ macfor.Source = (*Source)(nil)
