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
// traversal attacks. The result is lexical: it is used for error messages and
// to derive the root-relative name, never to open a file, because opening it
// would follow symlinks out of the root (see rootRel).
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

// rootRel returns path relative to root ("." for the root itself), after the
// lexical traversal guard of joinRoot.
//
// Every file operation then goes through an os.Root opened on the root, which
// resolves symlinks inside the tree but refuses any that lead outside it. The
// tree is evidence and may be hostile: a symlink such as
// Users/alice/Library -> /Users/investigator/Library would otherwise make the
// collector read, and record as evidence, files from the machine running it.
// A symlink that escapes the root fails with an error rather than being
// followed.
func rootRel(root, path string) (string, error) {
	joined, err := joinRoot(root, path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, joined)
	if err != nil {
		return "", macfor.WrapPermissionDenied(path, fmt.Errorf("path traversal detected"))
	}
	return rel, nil
}

// inRoot runs fn with root i opened as an os.Root and path made relative to it.
// The root is opened per call rather than held, so the Source keeps no file
// descriptor on the tree; an open descriptor on a mounted image would make its
// detach fail as busy.
func (s *Source) inRoot(i int, path string, fn func(r *os.Root, rel string) error) error {
	rel, err := rootRel(s.roots[i], path)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(s.roots[i])
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	err = fn(r, rel)
	// os.Root reports names relative to the root; restore the host path so
	// errors say which root and file they concern, as they did before.
	var pe *fs.PathError
	if errors.As(err, &pe) && !filepath.IsAbs(pe.Path) {
		pe.Path = filepath.Join(s.roots[i], pe.Path)
	}
	return err
}

// locate returns the index of the root that serves path: the first root in
// which the path exists. If a root reports an error other than absence (e.g.,
// permission denied, or a symlink escaping the root), that root is chosen so
// the caller surfaces the error rather than masking it with a later root. If no
// root has the path, the first root is chosen so the caller reports the first
// root's not-found error. The path-traversal guard is applied per root.
func (s *Source) locate(path string) (int, error) {
	if len(s.roots) == 1 {
		_, err := rootRel(s.roots[0], path)
		return 0, err
	}
	for i := range s.roots {
		var statErr error
		if err := s.inRoot(i, path, func(r *os.Root, rel string) error {
			_, statErr = r.Lstat(rel)
			return nil
		}); err != nil {
			return 0, err
		}
		if statErr == nil || !isAbsent(statErr) {
			return i, nil
		}
	}
	return 0, nil
}

// lstat returns the file information for path in the root that serves it,
// without following a final symlink.
func (s *Source) lstat(path string) (fs.FileInfo, error) {
	idx, err := s.locate(path)
	if err != nil {
		return nil, err
	}
	var info fs.FileInfo
	err = s.inRoot(idx, path, func(r *os.Root, rel string) error {
		var statErr error
		info, statErr = r.Lstat(rel)
		return statErr
	})
	return info, err
}

// isAbsent reports whether err means a path does not exist in a root. ENOTDIR
// (a path component is a regular file) counts as absent.
func isAbsent(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}

// Open opens the file at the given path for reading.
// The caller is responsible for closing the returned reader.
func (s *Source) Open(path string) (io.ReadCloser, error) {
	idx, err := s.locate(path)
	if err != nil {
		return nil, err
	}

	var f *os.File
	err = s.inRoot(idx, path, func(r *os.Root, rel string) error {
		var openErr error
		f, openErr = r.Open(rel)
		return openErr
	})
	if err != nil {
		return nil, wrapOSError("open", path, err)
	}
	return f, nil
}

// Stat returns file information for the given path.
// The returned FileInfo includes forensically relevant timestamps
// (access time, change time, birth time) when available on the platform.
func (s *Source) Stat(path string) (macfor.FileInfo, error) {
	info, err := s.lstat(path)
	if err != nil {
		return nil, wrapOSError("stat", path, err)
	}
	return newFileInfo(info), nil
}

// Readlink returns the target of the symlink at path without following it. The
// target is returned as stored and may point outside the tree.
func (s *Source) Readlink(path string) (string, error) {
	idx, err := s.locate(path)
	if err != nil {
		return "", err
	}
	var target string
	err = s.inRoot(idx, path, func(r *os.Root, rel string) error {
		var linkErr error
		target, linkErr = r.Readlink(rel)
		return linkErr
	})
	if err != nil {
		return "", wrapOSError("readlink", path, err)
	}
	return target, nil
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

// readDirIn lists path within root i, sorted by name.
func (s *Source) readDirIn(i int, path string) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	err := s.inRoot(i, path, func(r *os.Root, rel string) error {
		d, openErr := r.Open(rel)
		if openErr != nil {
			return openErr
		}
		defer func() { _ = d.Close() }()
		var readErr error
		entries, readErr = d.ReadDir(-1)
		return readErr
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	return entries, nil
}

// readDirMerged lists path across all roots, sorted by name. Errors from the
// serving root (see locate) are returned unwrapped, as are permission errors
// from later roots. Later roots that lack the directory, or have a non-directory
// at that path, are skipped.
func (s *Source) readDirMerged(path string) ([]fs.DirEntry, error) {
	idx, err := s.locate(path)
	if err != nil {
		return nil, err
	}

	entries, err := s.readDirIn(idx, path)
	if err != nil || idx == len(s.roots)-1 {
		return entries, err
	}

	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		seen[e.Name()] = struct{}{}
	}
	for i := idx + 1; i < len(s.roots); i++ {
		more, readErr := s.readDirIn(i, path)
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
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	return entries, nil
}

// Glob returns all files matching the pattern.
// The pattern uses source-absolute paths (e.g., /Users/*/Library/*.db)
// and results are returned as source-absolute paths. For a layered source
// matches come from the merged view, so each path appears once.
//
// The pattern is matched one path component at a time over the source's own
// directory listings, rather than with filepath.Glob on the host, which would
// follow symlinks out of the tree. Like filepath.Glob, I/O errors are ignored
// and the only possible error is a malformed pattern; matches are ordered by
// path component.
func (s *Source) Glob(pattern string) ([]string, error) {
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, macfor.WrapIOError("glob", pattern, err)
	}

	cleaned := filepath.ToSlash(filepath.Clean("/" + strings.TrimPrefix(filepath.ToSlash(pattern), "/")))
	matches := []string{"/"}
	for _, part := range strings.Split(strings.TrimPrefix(cleaned, "/"), "/") {
		if part == "" {
			continue
		}
		var next []string
		for _, dir := range matches {
			if !hasGlobMeta(part) {
				candidate := joinSource(dir, part)
				if _, err := s.lstat(candidate); err == nil {
					next = append(next, candidate)
				}
				continue
			}
			entries, err := s.readDirMerged(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if ok, _ := filepath.Match(part, e.Name()); ok {
					next = append(next, joinSource(dir, e.Name()))
				}
			}
		}
		matches = next
	}

	if len(matches) == 1 && matches[0] == "/" {
		return []string{}, nil
	}
	if matches == nil {
		return []string{}, nil
	}
	return matches, nil
}

// hasGlobMeta reports whether part contains a filepath.Match metacharacter.
func hasGlobMeta(part string) bool {
	return strings.ContainsAny(part, `*?[\`)
}

// joinSource appends name to the source-absolute directory dir.
func joinSource(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// Walk walks the file tree rooted at root, calling fn for each file
// or directory in the tree, including root. Paths passed to fn are
// source-absolute (with leading /). For a layered source every source path is
// visited once across all roots, the first root winning, and fs.SkipDir and
// fs.SkipAll follow filepath.WalkDir semantics over the merged tree. Symlinks
// are reported, never followed.
func (s *Source) Walk(root string, fn macfor.WalkFunc) error {
	// The join is lexical, so the start path is reported exactly as before
	// (e.g., "/." for "/").
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
	osInfo, statErr := s.lstat(sourcePath)
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
