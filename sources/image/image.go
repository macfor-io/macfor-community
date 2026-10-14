package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/directory"
)

// closeTimeout bounds the detach run by Close, which has no caller context.
const closeTimeout = 60 * time.Second

// attachFunc attaches the image at path in the given format. It has the
// signature of attachImage, so tests can substitute a fake attacher.
type attachFunc func(ctx context.Context, path string, format Format) (*attachment, error)

// Source is a macfor.Source over a disk image file (DMG, E01, or raw).
//
// It owns the image attachment and delegates all filesystem access to a
// directory.Source layered over the attached volumes: for a 10.15+ APFS volume
// group the Data volume first and the System volume second, so the image reads
// as one merged macOS filesystem; for a single-volume image, that one volume.
// Path resolution and traversal guards live solely in the directory source.
//
// The caller must call Close to detach the image; a leaked attachment holds the
// evidence file open.
type Source struct {
	// path is the absolute path of the original image file, recorded as
	// provenance; it is never a mount point.
	path string

	// format is the format detected from the image content.
	format Format

	// att is the hdiutil (and, for E01, ewfmount) attachment.
	att *attachment

	// dir serves the attached volumes as one filesystem.
	dir *directory.Source
}

// New detects the format of the image at imagePath, attaches it read-only
// (mounting it with ewfmount first for E01), and returns a Source over its
// mounted volumes. The caller must call Close on the returned Source.
//
// No attachment outlives a failed New: if the directory source cannot be built
// over the mounted volumes, the image is detached before the error returns.
func New(ctx context.Context, imagePath string) (*Source, error) {
	return newWith(ctx, imagePath, attachImage)
}

// newWith is New with the attach step injected.
func newWith(ctx context.Context, imagePath string, attachFn attachFunc) (*Source, error) {
	abs, err := filepath.Abs(imagePath)
	if err != nil {
		return nil, fmt.Errorf("open image %s: resolve path: %w", imagePath, err)
	}

	format, err := DetectFormat(abs)
	if err != nil {
		return nil, fmt.Errorf("open image %s: %w", abs, err)
	}

	a, err := attachFn(ctx, abs, format)
	if err != nil {
		return nil, fmt.Errorf("open image %s: %w", abs, err)
	}
	if a == nil {
		return nil, fmt.Errorf("open image %s: %w", abs, ErrNoMountedVolume)
	}

	dir, err := directory.NewLayered(a.roots()...)
	if err != nil {
		// The caller's ctx may already be done, so detach on a fresh one.
		return nil, errors.Join(fmt.Errorf("open image %s: %w", abs, err), a.cleanup())
	}

	return &Source{path: abs, format: format, att: a, dir: dir}, nil
}

// Close detaches the image and, for E01, unmounts the ewfmount FUSE mount and
// removes its mount dir. It is safe to call more than once: after a successful
// detach later calls are no-ops, and a failed detach can be retried.
func (s *Source) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()

	if err := s.att.detach(ctx); err != nil {
		return fmt.Errorf("close image %s: %w", s.path, err)
	}
	return nil
}

// ImagePath returns the absolute path of the original image file.
func (s *Source) ImagePath() string {
	return s.path
}

// Format returns the image format detected from the file content.
func (s *Source) Format() Format {
	return s.format
}

// MountPoint returns the mount point of the primary System volume: the volume
// carrying SystemVersion.plist, or the sole mounted volume of a single-volume
// image. For a split 10.15+ volume group this is the last entry of Roots, not
// the Data volume.
func (s *Source) MountPoint() string {
	return s.att.system
}

// Roots returns the mount points the source reads from, in resolution order:
// Data then System for a split 10.15+ volume group, or the single volume. The
// returned slice is a copy.
func (s *Source) Roots() []string {
	return append([]string(nil), s.att.roots()...)
}

// Type returns SourceTypeImage.
func (s *Source) Type() macfor.SourceType {
	return macfor.SourceTypeImage
}

// OSVersion returns the macOS version read from the image.
func (s *Source) OSVersion() (string, error) {
	return s.dir.OSVersion()
}

// Hostname returns the hostname read from the image.
func (s *Source) Hostname() (string, error) {
	return s.dir.Hostname()
}

// Open opens the file at the source-absolute path on the image.
func (s *Source) Open(path string) (io.ReadCloser, error) {
	return s.dir.Open(path)
}

// Stat returns file information for the source-absolute path on the image.
func (s *Source) Stat(path string) (macfor.FileInfo, error) {
	return s.dir.Stat(path)
}

// ReadDir lists the directory at the source-absolute path, merged across the
// mounted volumes.
func (s *Source) ReadDir(path string) ([]macfor.DirEntry, error) {
	return s.dir.ReadDir(path)
}

// Glob returns the source-absolute paths on the image matching pattern.
func (s *Source) Glob(pattern string) ([]string, error) {
	return s.dir.Glob(pattern)
}

// Walk walks the image file tree rooted at root, merged across the mounted
// volumes.
func (s *Source) Walk(root string, fn macfor.WalkFunc) error {
	return s.dir.Walk(root, fn)
}

// Users returns the human user accounts found on the image.
func (s *Source) Users() ([]macfor.User, error) {
	return s.dir.Users()
}

// ResolvePath resolves path variables such as ~ against the image.
func (s *Source) ResolvePath(path string) string {
	return s.dir.ResolvePath(path)
}

// ReadPlist reads and decodes the plist at the source-absolute path into v.
func (s *Source) ReadPlist(path string, v any) error {
	return s.dir.ReadPlist(path, v)
}

var (
	_ macfor.Source = (*Source)(nil)
	_ io.Closer     = (*Source)(nil)
)
