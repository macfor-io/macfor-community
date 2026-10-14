package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/image"
)

// Chain-of-custody vocabulary for image lifecycle entries.
const (
	// custodyActor is the actor recorded on image lifecycle entries.
	custodyActor = "macfor"

	// custodyKeyImagePath is the detail key for the original image file.
	custodyKeyImagePath = "image_path"
)

// errImageAndFrom is returned when both --image and --from are given.
var errImageAndFrom = errors.New("--image and --from are mutually exclusive: collect from a disk image file or a directory tree, not both")

// imageSource is the part of *image.Source the CLI drives: a macfor.Source
// that must be closed to detach the image, plus the provenance it reports.
type imageSource interface {
	macfor.Source
	io.Closer

	// ImagePath returns the absolute path of the original image file.
	ImagePath() string

	// Format returns the image format detected from the file content.
	Format() image.Format

	// MountPoint returns the mount point of the primary System volume.
	MountPoint() string

	// Roots returns the mount points the source reads from.
	Roots() []string

	// Warnings describes content of the image that collection cannot reach.
	Warnings() []string
}

// newImageSource attaches the disk image at path read-only and returns a
// source over its volumes. It is a variable so tests can substitute a fake and
// never run a real attach.
var newImageSource = func(ctx context.Context, path string) (imageSource, error) {
	src, err := image.New(ctx, path)
	if err != nil {
		return nil, err
	}
	return src, nil
}

// validateImagePath checks that path names an existing regular file, so a
// mistyped or directory path fails with a clear message before any attach.
func validateImagePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("image file does not exist: %s", path)
		}
		return fmt.Errorf("checking image path: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("image path is a directory, not a disk image file: %s (use --from to collect from a directory tree)", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("image path is not a regular file: %s", path)
	}
	return nil
}

// sourceHandle owns the release of a collection source. For a source that
// holds resources (an attached disk image, which implements io.Closer) it
// detaches exactly once however many times Close is called; for live and
// directory sources Close is a no-op.
type sourceHandle struct {
	src macfor.Source

	// closer is the source's io.Closer, or nil if it holds no resources.
	closer io.Closer

	// openedAt is when the source was opened (the image attached).
	openedAt time.Time

	mu     sync.Mutex
	closed bool
}

// newSourceHandle wraps src, recording now as the time it was opened.
func newSourceHandle(src macfor.Source) *sourceHandle {
	h := &sourceHandle{src: src, openedAt: time.Now()}
	if c, ok := src.(io.Closer); ok {
		h.closer = c
	}
	return h
}

// Close releases the source. After a successful close later calls are no-ops;
// after a failed one the next call retries, so the deferred safety net in
// runCollect gets a second attempt at a detach that failed before finalize.
func (h *sourceHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closer == nil || h.closed {
		return nil
	}
	if err := h.closer.Close(); err != nil {
		return err
	}
	h.closed = true
	return nil
}

// warnings returns what an image source reports it cannot reach, or nil.
func (h *sourceHandle) warnings() []string {
	if img, ok := h.src.(imageSource); ok {
		return img.Warnings()
	}
	return nil
}

// describe returns how the dry-run output names the source, or "" for the
// live system.
func (h *sourceHandle) describe(fromPath string) string {
	if img, ok := h.src.(imageSource); ok {
		return fmt.Sprintf("image (%s, %s)", img.ImagePath(), img.Format())
	}
	if fromPath != "" {
		absFrom, _ := filepath.Abs(fromPath)
		return fmt.Sprintf("directory (%s)", absFrom)
	}
	return ""
}

// custodyHooks returns the orchestrator hooks that record an image source's
// lifecycle in the chain of custody, or nil hooks for any other source.
//
// The custody log is written when the container is finalized, so a detach
// left to runCollect's deferred Close would never be recorded. Instead the
// attach is recorded once the container's custody log exists (onStart, with
// the real attach time as its timestamp), and the image is detached and the
// outcome recorded after collection but before finalize (beforeFinalize).
func (h *sourceHandle) custodyHooks() (onStart, beforeFinalize func(*container.Container)) {
	img, ok := h.src.(imageSource)
	if !ok {
		return nil, nil
	}

	imagePath := img.ImagePath()

	onStart = func(c *container.Container) {
		details := map[string]any{
			custodyKeyImagePath: imagePath,
			"image_format":      string(img.Format()),
			"mount_point":       img.MountPoint(),
			"roots":             img.Roots(),
			"read_only":         true,
		}
		// The image file's size and modification time tie the collection to
		// the exact copy that was attached, without hashing an image that may
		// be hundreds of gigabytes.
		if info, err := os.Stat(imagePath); err == nil {
			details["image_size_bytes"] = info.Size()
			details["image_modified"] = info.ModTime().UTC()
		}
		if warnings := img.Warnings(); len(warnings) > 0 {
			details["warnings"] = warnings
		}
		c.AddCustodyEntry(container.ChainOfCustodyEntry{
			Timestamp: h.openedAt,
			Action:    container.CustodyActionImageAttached,
			Actor:     custodyActor,
			Target:    imagePath,
			Details:   details,
		})
	}

	beforeFinalize = func(c *container.Container) {
		if err := h.Close(); err != nil {
			logger.Warn().Err(err).Str("image", imagePath).Msg("Detaching image")
			c.AddCustodyEntry(container.ChainOfCustodyEntry{
				Action: container.CustodyActionImageDetachFailed,
				Actor:  custodyActor,
				Target: imagePath,
				Details: map[string]any{
					custodyKeyImagePath: imagePath,
					"error":             err.Error(),
				},
			})
			return
		}
		c.AddCustodyEntry(container.ChainOfCustodyEntry{
			Action: container.CustodyActionImageDetached,
			Actor:  custodyActor,
			Target: imagePath,
			Details: map[string]any{
				custodyKeyImagePath: imagePath,
			},
		})
	}

	return onStart, beforeFinalize
}
