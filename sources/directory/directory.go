// Package directory provides a Source implementation that reads from a directory tree.
// This is used for forensic analysis of mounted disk images, snapshots, or test fixtures.
// Unlike the live source, this implementation is cross-platform and works on both
// macOS and Linux.
package directory

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Source provides access to a directory tree as a forensic data source.
// It implements the macfor.Source interface by mapping source-absolute paths
// (e.g., /Users/admin/Library) to host-relative paths under the configured root.
type Source struct {
	// root is the absolute path to the directory tree on the host filesystem.
	root string
}

// New creates a new Source rooted at the given directory path.
// The path must exist and be a directory. Returns ErrNotFound if the path
// does not exist, or an error if it is not a directory.
func New(root string) (*Source, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, macfor.WrapNotFound(abs, err)
		}
		return nil, macfor.WrapIOError("stat", abs, err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", abs)
	}

	return &Source{root: abs}, nil
}

// Type returns SourceTypeDirectory indicating this is a directory tree source.
func (s *Source) Type() macfor.SourceType {
	return macfor.SourceTypeDirectory
}

// resolve translates a source-absolute path to a host-absolute path under the root.
// It enforces that the resolved path stays within the root directory to prevent
// path traversal attacks.
func (s *Source) resolve(path string) (string, error) {
	// Strip leading slash before joining.
	stripped := strings.TrimPrefix(filepath.ToSlash(path), "/")
	resolved := filepath.Join(s.root, filepath.FromSlash(stripped))

	// Verify the resolved path stays within root.
	rel, err := filepath.Rel(s.root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", macfor.WrapPermissionDenied(path, fmt.Errorf("path traversal detected"))
	}

	return resolved, nil
}

// Open opens the file at the given path for reading.
// The caller is responsible for closing the returned reader.
func (s *Source) Open(path string) (io.ReadCloser, error) {
	resolved, err := s.resolve(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(resolved) //nolint:gosec // G304: Intentional file access for forensic collection.
	if err != nil {
		return nil, wrapOSError("open", path, err)
	}
	return f, nil
}

// Stat returns file information for the given path.
// The returned FileInfo includes forensically relevant timestamps
// (access time, change time, birth time) when available on the platform.
func (s *Source) Stat(path string) (macfor.FileInfo, error) {
	resolved, err := s.resolve(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, wrapOSError("stat", path, err)
	}
	return newFileInfo(info), nil
}

// ReadDir reads the directory at the given path and returns
// a list of directory entries.
func (s *Source) ReadDir(path string) ([]macfor.DirEntry, error) {
	resolved, err := s.resolve(path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, wrapOSError("readdir", path, err)
	}

	result := make([]macfor.DirEntry, len(entries))
	for i, e := range entries {
		result[i] = e
	}
	return result, nil
}

// Glob returns all files matching the pattern.
// The pattern uses source-absolute paths (e.g., /Users/*/Library/*.db)
// and results are returned as source-absolute paths.
func (s *Source) Glob(pattern string) ([]string, error) {
	// Strip leading slash and join to root.
	stripped := strings.TrimPrefix(filepath.ToSlash(pattern), "/")
	rootedPattern := filepath.Join(s.root, filepath.FromSlash(stripped))

	matches, err := filepath.Glob(rootedPattern)
	if err != nil {
		return nil, macfor.WrapIOError("glob", pattern, err)
	}

	// Translate back to source-absolute paths.
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		rel, relErr := filepath.Rel(s.root, m)
		if relErr != nil {
			continue
		}
		result = append(result, "/"+filepath.ToSlash(rel))
	}
	return result, nil
}

// Walk walks the file tree rooted at root, calling fn for each file
// or directory in the tree, including root. Paths passed to fn are
// source-absolute (with leading /).
func (s *Source) Walk(root string, fn macfor.WalkFunc) error {
	resolved, err := s.resolve(root)
	if err != nil {
		return err
	}

	return filepath.WalkDir(resolved, func(hostPath string, _ fs.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(s.root, hostPath)
		if relErr != nil {
			return fmt.Errorf("relativize path: %w", relErr)
		}
		sourcePath := "/" + filepath.ToSlash(rel)

		var info macfor.FileInfo
		if walkErr == nil {
			// Get full file info including forensic timestamps.
			osInfo, statErr := os.Lstat(hostPath)
			if statErr != nil {
				walkErr = wrapOSError("stat", sourcePath, statErr)
			} else {
				info = newFileInfo(osInfo)
			}
		} else {
			walkErr = wrapOSError("walk", sourcePath, walkErr)
		}

		return fn(sourcePath, info, walkErr)
	})
}

// ResolvePath resolves path variables like ~ to actual paths.
// For directory sources, ~ is resolved using the first user returned by Users().
// If user enumeration fails or returns no users, the path is returned unchanged.
func (s *Source) ResolvePath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	// Try to resolve ~ using enumerated users.
	users, err := s.Users()
	if err != nil || len(users) == 0 {
		return path
	}

	// Handle ~/... (first user).
	if path == "~" || strings.HasPrefix(path, "~/") {
		return filepath.Join(users[0].HomeDir, strings.TrimPrefix(path, "~"))
	}

	// Handle ~username/... (specific user).
	rest := strings.TrimPrefix(path, "~")
	var username, suffix string
	if idx := strings.Index(rest, "/"); idx != -1 {
		username = rest[:idx]
		suffix = rest[idx:]
	} else {
		username = rest
	}

	for _, u := range users {
		if u.Username == username {
			return filepath.Join(u.HomeDir, suffix)
		}
	}

	// User not found, return path unchanged.
	return path
}

// ReadPlist reads and parses a plist file into the provided value.
// The value should be a pointer to a struct or map that matches
// the plist structure. Supports both XML and binary plist formats.
func (s *Source) ReadPlist(path string, v any) error {
	f, err := s.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Read all content since plist.NewDecoder requires io.ReadSeeker.
	data, err := io.ReadAll(f)
	if err != nil {
		return macfor.WrapIOError("read", path, err)
	}

	decoder := plist.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(v); err != nil {
		return macfor.WrapParseError(path, 0, 0, "plist decode failed", err)
	}

	return nil
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
var _ macfor.Source = (*Source)(nil)
