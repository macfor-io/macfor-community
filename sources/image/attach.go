package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"howett.net/plist"
)

// ErrUnsupportedPlatform indicates disk images cannot be attached on this OS,
// because attaching relies on hdiutil, which exists only on macOS.
var ErrUnsupportedPlatform = errors.New("disk image attach is only supported on macOS")

// ErrNoMountedVolume indicates hdiutil attached the image but mounted no
// volume, so there is nothing to collect from.
var ErrNoMountedVolume = errors.New("disk image attached but no volume was mounted")

const (
	// hdiutilPath is the absolute path of hdiutil, so attach never depends on
	// PATH, which an investigator's shell may have altered.
	hdiutilPath = "/usr/bin/hdiutil"

	// cleanupTimeout bounds the detach run after the caller's context is done.
	cleanupTimeout = 30 * time.Second

	// systemVersionPlist marks the System volume of a macOS install.
	systemVersionPlist = "System/Library/CoreServices/SystemVersion.plist"
)

// dataVolumeMarkers are entries whose presence at a volume root marks the Data
// volume of a 10.15+ APFS volume group.
var dataVolumeMarkers = []string{"Users", "private/var"}

// apfsContainerHint is the content hint hdiutil reports for a synthesized APFS
// container disk (the APFS container partition type GUID).
const apfsContainerHint = "EF57347C-0000-11AA-AA11-00306543ECAC"

// wholeDiskPattern matches a top-level disk dev-entry such as /dev/disk4, as
// opposed to a slice such as /dev/disk4s1.
var wholeDiskPattern = regexp.MustCompile(`^/dev/disk[0-9]+$`)

// sliceSuffixPattern matches the slice suffix of a dev-entry, so /dev/disk4s1
// and /dev/disk4s1s1 both reduce to /dev/disk4.
var sliceSuffixPattern = regexp.MustCompile(`^(/dev/disk[0-9]+)s[0-9].*$`)

// commandRunner runs an external command and returns its standard output. It
// exists so attach and mount logic can be tested without running real tools.
type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout []byte, err error)
}

// execRunner is the production commandRunner backed by os/exec.
type execRunner struct{}

// Run executes name with args, killing it if ctx is done. On failure the
// returned error carries the command's stderr text, not just its exit status.
func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // Callers pass fixed tool paths; args are not shell-interpreted.
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return stdout.Bytes(), fmt.Errorf("%s %s: %w", filepath.Base(name), strings.Join(args, " "), err)
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s", filepath.Base(name), strings.Join(args, " "), err, msg)
	}
	return stdout.Bytes(), nil
}

// hdiutilEntity is one entry of the system-entities array in the plist that
// hdiutil attach -plist prints. Partition-scheme and container entries carry no
// mount-point.
type hdiutilEntity struct {
	DevEntry    string `plist:"dev-entry"`
	MountPoint  string `plist:"mount-point"`
	ContentHint string `plist:"content-hint"`
	VolumeKind  string `plist:"volume-kind"`
}

// hdiutilAttachOutput is the top-level plist printed by hdiutil attach -plist.
type hdiutilAttachOutput struct {
	SystemEntities []hdiutilEntity `plist:"system-entities"`
}

// parseAttachPlist decodes the system-entities array from hdiutil attach -plist
// output. Anything printed before the XML declaration is ignored.
func parseAttachPlist(out []byte) ([]hdiutilEntity, error) {
	if i := bytes.Index(out, []byte("<?xml")); i > 0 {
		out = out[i:]
	}

	var parsed hdiutilAttachOutput
	if _, err := plist.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse hdiutil attach output: %w", err)
	}
	if len(parsed.SystemEntities) == 0 {
		return nil, errors.New("parse hdiutil attach output: no system-entities")
	}
	return parsed.SystemEntities, nil
}

// topLevelDevNode returns the dev-entry to pass to hdiutil detach.
//
// Detaching the top-level image disk detaches every slice and every volume
// mounted from it, whereas detaching a slice leaves the image attached. hdiutil
// does not list entities in a fixed order: a captured HFS+ attach lists the
// slice /dev/disk4s1 before the whole disk /dev/disk4, and an APFS attach lists
// /dev/disk4, /dev/disk5s1, /dev/disk4s1, /dev/disk5, where /dev/disk5 is the
// synthesized APFS container (content hint apfsContainerHint). The container is
// torn down with the image disk, so the target is the first whole disk that is
// not a synthesized container, then the first whole disk of any kind. If
// hdiutil reported no whole disk at all, the slice suffix of the first entry is
// stripped instead.
func topLevelDevNode(entities []hdiutilEntity) string {
	for _, e := range entities {
		if wholeDiskPattern.MatchString(e.DevEntry) && e.ContentHint != apfsContainerHint {
			return e.DevEntry
		}
	}
	for _, e := range entities {
		if wholeDiskPattern.MatchString(e.DevEntry) {
			return e.DevEntry
		}
	}
	for _, e := range entities {
		if m := sliceSuffixPattern.FindStringSubmatch(e.DevEntry); m != nil {
			return m[1]
		}
	}
	return ""
}

// attachment is a disk image attached by hdiutil. It records the device to
// detach and the mounted volumes that collection reads from.
type attachment struct {
	runner commandRunner

	// devNode is the top-level dev-entry passed to hdiutil detach.
	devNode string

	// system is the mount point of the System volume, or of the first mounted
	// volume when no System volume was recognised.
	system string

	// data is the mount point of the separate Data volume of a 10.15+ APFS
	// volume group, or empty for a single-volume image.
	data string

	mu       sync.Mutex
	detached bool
}

// newAttachment builds an attachment from parsed hdiutil entities, classifying
// the mounted volumes by inspecting the filesystem under each mount point.
func newAttachment(runner commandRunner, entities []hdiutilEntity) *attachment {
	a := &attachment{runner: runner, devNode: topLevelDevNode(entities)}

	var mounts []string
	for _, e := range entities {
		if e.MountPoint != "" {
			mounts = append(mounts, e.MountPoint)
		}
	}

	for _, mp := range mounts {
		if exists(filepath.Join(mp, systemVersionPlist)) {
			a.system = mp
			break
		}
	}

	if a.system == "" {
		// No recognisable macOS install; collect from whatever mounted first.
		if len(mounts) > 0 {
			a.system = mounts[0]
		}
		return a
	}

	for _, mp := range mounts {
		if mp != a.system && isDataVolume(mp) {
			a.data = mp
			break
		}
	}
	return a
}

// isDataVolume reports whether the volume mounted at mp looks like the Data
// volume of an APFS volume group.
func isDataVolume(mp string) bool {
	for _, marker := range dataVolumeMarkers {
		if exists(filepath.Join(mp, marker)) {
			return true
		}
	}
	return false
}

// exists reports whether path exists, without following a final symlink out of
// the image.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// mounted reports whether the attachment has any volume to collect from.
func (a *attachment) mounted() bool {
	return a.system != ""
}

// roots returns the collection roots in resolution order: Data before System for
// a split 10.15+ volume group, so user content resolves against the Data volume,
// or the single mounted volume otherwise.
func (a *attachment) roots() []string {
	if a.data != "" {
		return []string{a.data, a.system}
	}
	if a.system != "" {
		return []string{a.system}
	}
	return nil
}

// detach detaches the image, retrying once with -force when the plain detach
// fails (typically because a volume is busy). Calling detach again after it has
// succeeded is a no-op.
func (a *attachment) detach(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.detached {
		return nil
	}
	if a.devNode == "" {
		return errors.New("detach image: no device node recorded")
	}

	if _, err := a.runner.Run(ctx, hdiutilPath, "detach", a.devNode); err != nil {
		if _, forceErr := a.runner.Run(ctx, hdiutilPath, "detach", "-force", a.devNode); forceErr != nil {
			return fmt.Errorf("detach %s: %w (forced retry: %w)", a.devNode, err, forceErr)
		}
	}

	a.detached = true
	return nil
}

// attachArgs returns the hdiutil arguments that attach path read-only, without
// surfacing the volumes in Finder.
func attachArgs(path string, format Format) []string {
	args := []string{"attach", "-readonly", "-nobrowse", "-plist"}
	if format == FormatRaw {
		// hdiutil does not recognise an unwrapped image by itself.
		args = append(args, "-imagekey", "diskimage-class=CRawDiskImage")
	}
	return append(args, path)
}

// attachWith attaches the image at path using runner and returns the resulting
// attachment. Every failure after hdiutil reports an attached device detaches it
// again, so no attachment leaks; that includes ctx being cancelled while attach
// ran.
func attachWith(ctx context.Context, runner commandRunner, path string, format Format) (*attachment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out, err := runner.Run(ctx, hdiutilPath, attachArgs(path, format)...)
	if err != nil {
		return nil, fmt.Errorf("attach %s: %w", path, err)
	}

	entities, err := parseAttachPlist(out)
	if err != nil {
		return nil, fmt.Errorf("attach %s: %w", path, err)
	}

	a := newAttachment(runner, entities)

	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, a.cleanup())
	}
	if !a.mounted() {
		return nil, errors.Join(fmt.Errorf("attach %s: %w", path, ErrNoMountedVolume), a.cleanup())
	}
	return a, nil
}

// cleanup detaches on a fresh context, for use when the caller's context may
// already be done.
func (a *attachment) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	return a.detach(ctx)
}
