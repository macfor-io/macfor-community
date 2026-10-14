package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ErrEWFMountNotFound indicates the ewfmount binary needed to open EnCase
// E01/Ex01 images is not on PATH. It is an optional runtime prerequisite, so its
// absence affects only E01 images; DMG and raw images attach without it.
var ErrEWFMountNotFound = errors.New("ewfmount not found on PATH: E01 images need libewf (brew install libewf) and macFUSE")

const (
	// ewfmountName is the binary resolved on PATH to expose an EWF image.
	ewfmountName = "ewfmount"

	// umountPath is the absolute path of umount, so unmount never depends on
	// PATH, which an investigator's shell may have altered.
	umountPath = "/sbin/umount"

	// ewfDeviceName is the raw device file ewfmount exposes inside its mount dir.
	ewfDeviceName = "ewf1"

	// ewfMountDirPattern names the private temp dir ewfmount mounts onto.
	ewfMountDirPattern = "macfor-ewf-*"
)

// ewfTools bundles the external dependencies of the E01 mount, so tests can run
// the whole lifecycle without ewfmount, umount, or hdiutil.
type ewfTools struct {
	// runner runs ewfmount, umount, and hdiutil.
	runner commandRunner

	// lookPath resolves the ewfmount binary; production uses exec.LookPath.
	lookPath func(file string) (string, error)

	// removeAll deletes the mount dir after unmounting; production uses
	// os.RemoveAll.
	removeAll func(path string) error

	// tempDir is the parent of the mount dir; empty means os.TempDir.
	tempDir string
}

// ewfMount is an EnCase image exposed as a raw device file by ewfmount, which
// FUSE-mounts the EWF container onto a private temp dir. The caller must call
// unmount, directly or through the owning attachment's detach.
type ewfMount struct {
	runner    commandRunner
	removeAll func(path string) error

	// dir is the private (0700) temp dir ewfmount mounted onto.
	dir string

	mu sync.Mutex

	// mounted is true while the FUSE mount on dir is live.
	mounted bool

	// removed is true once dir has been deleted.
	removed bool
}

// device returns the path of the raw device file ewfmount exposes, which is
// attached with hdiutil as a raw image.
func (m *ewfMount) device() string {
	return filepath.Join(m.dir, ewfDeviceName)
}

// mountEWF exposes the EWF image at imagePath as a raw device with ewfmount.
//
// For a segmented set (.E01, .E02, ...) pass the first segment: ewfmount locates
// the remaining segments beside it by name. The image is only ever read.
//
// It fails with ErrEWFMountNotFound when ewfmount is not on PATH. Any failure
// after the mount dir is created unwinds what succeeded, so neither a FUSE mount
// nor a temp dir leaks.
func mountEWF(ctx context.Context, tools ewfTools, imagePath string) (*ewfMount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ewfmount, err := tools.lookPath(ewfmountName)
	if err != nil {
		return nil, fmt.Errorf("mount %s: %w (%w)", imagePath, ErrEWFMountNotFound, err)
	}

	// MkdirTemp creates the dir with mode 0700, so only the collector can reach
	// the exposed evidence.
	dir, err := os.MkdirTemp(tools.tempDir, ewfMountDirPattern)
	if err != nil {
		return nil, fmt.Errorf("mount %s: create mount dir: %w", imagePath, err)
	}

	m := &ewfMount{runner: tools.runner, removeAll: tools.removeAll, dir: dir}

	if _, err := tools.runner.Run(ctx, ewfmount, imagePath, dir); err != nil {
		// Nothing is mounted, so only the dir needs removing.
		m.removed = true
		return nil, errors.Join(fmt.Errorf("mount %s: %w", imagePath, err), m.removeAll(dir))
	}
	m.mounted = true

	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, m.cleanup())
	}
	if _, err := os.Stat(m.device()); err != nil {
		return nil, errors.Join(fmt.Errorf("mount %s: ewfmount exposed no %s device: %w", imagePath, ewfDeviceName, err), m.cleanup())
	}
	return m, nil
}

// unmount unmounts the FUSE mount, retrying once with -f when the plain umount
// fails, then removes the mount dir. The dir is removed only after a successful
// unmount, so removal can never reach into a still-mounted image. Calling
// unmount again after it has succeeded is a no-op, and a failed unmount can be
// retried.
func (m *ewfMount) unmount(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mounted {
		if _, err := m.runner.Run(ctx, umountPath, m.dir); err != nil {
			if _, forceErr := m.runner.Run(ctx, umountPath, "-f", m.dir); forceErr != nil {
				return fmt.Errorf("unmount %s: %w (forced retry: %w)", m.dir, err, forceErr)
			}
		}
		m.mounted = false
	}

	if !m.removed {
		if err := m.removeAll(m.dir); err != nil {
			return fmt.Errorf("remove mount dir %s: %w", m.dir, err)
		}
		m.removed = true
	}
	return nil
}

// cleanup unmounts on a fresh context, for use when the caller's context may
// already be done.
func (m *ewfMount) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return m.unmount(ctx)
}

// attachEWFWith performs the two-stage E01 attach: ewfmount exposes the image as
// a raw device, which hdiutil then attaches read-only as FormatRaw. The returned
// attachment owns the ewf mount, so a single detach tears down in strict reverse
// order: hdiutil detach, then umount, then removal of the mount dir. A failed
// hdiutil attach unmounts and removes the dir before returning.
func attachEWFWith(ctx context.Context, tools ewfTools, imagePath string) (*attachment, error) {
	m, err := mountEWF(ctx, tools, imagePath)
	if err != nil {
		return nil, err
	}

	a, err := attachWith(ctx, tools.runner, m.device(), FormatRaw)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("attach %s: %w", imagePath, err), m.cleanup())
	}

	a.ewf = m
	return a, nil
}

// attachImage attaches the image at path in the given format, mounting it with
// ewfmount first when the format requires it. The caller must call detach on
// the returned attachment, which also releases any ewf mount.
func attachImage(ctx context.Context, path string, format Format) (*attachment, error) {
	if format.RequiresEWFMount() {
		return attachEWF(ctx, path)
	}
	return attach(ctx, path, format)
}
