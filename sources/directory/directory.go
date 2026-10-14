// Package directory provides a Source implementation that reads from a directory tree.
// This is used for forensic analysis of mounted disk images, snapshots, or test fixtures.
// Unlike the live source, this implementation is cross-platform and works on both
// macOS and Linux.
package directory

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Source provides access to a directory tree as a forensic data source.
// It implements the macfor.Source interface by mapping source-absolute paths
// (e.g., /Users/admin/Library) to host-relative paths under the configured root.
//
// A Source may also be layered over an ordered list of roots (see NewLayered),
// presenting them as one filesystem. This emulates the firmlinks that join the
// read-only System volume and the Data volume of a macOS 10.15+ APFS volume group
// when both are mounted separately from a disk image.
type Source struct {
	// root is the absolute path to the primary (first) directory tree on the host
	// filesystem. It is reported as the image path in SystemInfo.
	root string
	// roots is the ordered list of absolute root paths; roots[0] == root.
	// Earlier roots take precedence over later ones.
	roots []string
}

// New creates a new Source rooted at the given directory path.
// The path must exist and be a directory. Returns ErrNotFound if the path
// does not exist, or an error if it is not a directory.
func New(root string) (*Source, error) {
	return NewLayered(root)
}

// NewLayered creates a Source that serves an ordered list of directory trees as
// one filesystem. A path resolves against the first root in which it exists;
// directory listings, globs, and walks merge all roots, with the first root
// winning when the same name exists in several. Every root must exist and be a
// directory. NewLayered with a single root is equivalent to New.
func NewLayered(roots ...string) (*Source, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("no root directories given")
	}

	abs := make([]string, 0, len(roots))
	for _, r := range roots {
		a, err := validateRoot(r)
		if err != nil {
			return nil, err
		}
		abs = append(abs, a)
	}

	return &Source{root: abs[0], roots: abs}, nil
}

// validateRoot returns the absolute form of root after checking that it exists
// and is a directory.
func validateRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", macfor.WrapNotFound(abs, err)
		}
		return "", macfor.WrapIOError("stat", abs, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}

	return abs, nil
}

// Type returns SourceTypeDirectory indicating this is a directory tree source.
func (s *Source) Type() macfor.SourceType {
	return macfor.SourceTypeDirectory
}

// joinRoot translates a source-absolute path to a host-absolute path under root.
// It enforces that the resolved path stays within root to prevent path
// traversal attacks.
func joinRoot(root, path string) (string, error) {
	// Strip leading slash before joining.
	stripped := strings.TrimPrefix(filepath.ToSlash(path), "/")
	resolved := filepath.Join(root, filepath.FromSlash(stripped))

	// Verify the resolved path stays within root.
	rel, err := filepath.Rel(root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", macfor.WrapPermissionDenied(path, fmt.Errorf("path traversal detected"))
	}

	return resolved, nil
}

// resolve translates a source-absolute path to a host-absolute path.
// See locate for how a root is chosen.
func (s *Source) resolve(path string) (string, error) {
	_, resolved, err := s.locate(path)
	return resolved, err
}

// locate returns the index of the root that serves path and the host path in
// that root. The serving root is the first root in which the path exists. If a
// root reports an error other than absence (e.g., permission denied), that root
// is chosen so the caller surfaces the error rather than masking it with a
// later root. If no root has the path, the first root is chosen so the caller
// reports the first root's not-found error. The path-traversal guard is applied
// per root.
func (s *Source) locate(path string) (int, string, error) {
	var first string
	for i, r := range s.roots {
		resolved, err := joinRoot(r, path)
		if err != nil {
			return 0, "", err
		}
		if len(s.roots) == 1 {
			return 0, resolved, nil
		}
		if i == 0 {
			first = resolved
		}
		if _, err := os.Lstat(resolved); err == nil || !isAbsent(err) {
			return i, resolved, nil
		}
	}
	return 0, first, nil
}

// isAbsent reports whether err means a path does not exist in a root. ENOTDIR
// (a path component is a regular file) counts as absent.
func isAbsent(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
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
// a list of directory entries. For a layered source the entries of every root
// that has the directory are merged by name, the first root winning.
func (s *Source) ReadDir(path string) ([]macfor.DirEntry, error) {
	entries, err := s.readDirMerged(path)
	if err != nil {
		if errors.Is(err, macfor.ErrPermissionDenied) {
			return nil, err
		}
		return nil, wrapOSError("readdir", path, err)
	}

	result := make([]macfor.DirEntry, len(entries))
	for i, e := range entries {
		result[i] = e
	}
	return result, nil
}

// readDirMerged lists path across all roots, sorted by name. Errors from the
// serving root (see locate) are returned unwrapped, as are permission errors
// from later roots. Later roots that lack the directory, or have a non-directory
// at that path, are skipped.
func (s *Source) readDirMerged(path string) ([]fs.DirEntry, error) {
	idx, resolved, err := s.locate(path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(resolved)
	if err != nil || idx == len(s.roots)-1 {
		return entries, err
	}

	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		seen[e.Name()] = struct{}{}
	}
	for _, r := range s.roots[idx+1:] {
		hostPath, joinErr := joinRoot(r, path)
		if joinErr != nil {
			return nil, joinErr
		}
		more, readErr := os.ReadDir(hostPath)
		if readErr != nil {
			if os.IsPermission(readErr) {
				return nil, readErr
			}
			continue
		}
		for _, e := range more {
			if _, dup := seen[e.Name()]; dup {
				continue
			}
			seen[e.Name()] = struct{}{}
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// Glob returns all files matching the pattern.
// The pattern uses source-absolute paths (e.g., /Users/*/Library/*.db)
// and results are returned as source-absolute paths. For a layered source the
// result is the de-duplicated, sorted union of matches across all roots.
func (s *Source) Glob(pattern string) ([]string, error) {
	// Strip leading slash.
	stripped := strings.TrimPrefix(filepath.ToSlash(pattern), "/")

	result := []string{}
	seen := make(map[string]struct{})
	for _, r := range s.roots {
		rootedPattern := filepath.Join(r, filepath.FromSlash(stripped))

		matches, err := filepath.Glob(rootedPattern)
		if err != nil {
			return nil, macfor.WrapIOError("glob", pattern, err)
		}

		// Translate back to source-absolute paths.
		for _, m := range matches {
			rel, relErr := filepath.Rel(r, m)
			if relErr != nil {
				continue
			}
			sourcePath := "/" + filepath.ToSlash(rel)
			if _, dup := seen[sourcePath]; dup {
				continue
			}
			seen[sourcePath] = struct{}{}
			result = append(result, sourcePath)
		}
	}

	if len(s.roots) > 1 {
		sort.Strings(result)
	}
	return result, nil
}

// Walk walks the file tree rooted at root, calling fn for each file
// or directory in the tree, including root. Paths passed to fn are
// source-absolute (with leading /). For a layered source every source path is
// visited once across all roots, the first root winning, and fs.SkipDir and
// fs.SkipAll follow filepath.WalkDir semantics over the merged tree.
func (s *Source) Walk(root string, fn macfor.WalkFunc) error {
	if len(s.roots) > 1 {
		return s.walkLayered(root, fn)
	}

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

// walkLayered walks the merged tree of a layered source starting at root.
func (s *Source) walkLayered(root string, fn macfor.WalkFunc) error {
	// The join is lexical and identical in every root, so the primary root
	// yields the same source path the single-root walk reports (e.g., "/." for "/").
	hostPath, err := joinRoot(s.root, root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(s.root, hostPath)
	if err != nil {
		return fmt.Errorf("relativize path: %w", err)
	}

	err = s.walkMerged("/"+filepath.ToSlash(rel), fn)
	if errors.Is(err, fs.SkipDir) || errors.Is(err, fs.SkipAll) {
		return nil
	}
	return err
}

// walkMerged visits sourcePath and, if it is a directory, its merged children.
// It mirrors the control flow of filepath.WalkDir.
func (s *Source) walkMerged(sourcePath string, fn macfor.WalkFunc) error {
	resolved, err := s.resolve(sourcePath)
	if err != nil {
		return err
	}

	osInfo, statErr := os.Lstat(resolved)
	if statErr != nil {
		return fn(sourcePath, nil, wrapOSError("walk", sourcePath, statErr))
	}

	isDir := osInfo.IsDir()
	if err := fn(sourcePath, newFileInfo(osInfo), nil); err != nil || !isDir {
		if errors.Is(err, fs.SkipDir) && isDir {
			err = nil
		}
		return err
	}

	entries, readErr := s.readDirMerged(sourcePath)
	if readErr != nil {
		if err := fn(sourcePath, newFileInfo(osInfo), wrapOSError("walk", sourcePath, readErr)); err != nil {
			if errors.Is(err, fs.SkipDir) {
				err = nil
			}
			return err
		}
	}

	for _, e := range entries {
		if err := s.walkMerged(filepath.ToSlash(filepath.Join(sourcePath, e.Name())), fn); err != nil {
			if errors.Is(err, fs.SkipDir) {
				break
			}
			return err
		}
	}
	return nil
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
