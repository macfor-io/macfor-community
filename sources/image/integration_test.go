package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/directory"
	"github.com/macfor-io/macfor-community/testdata/macostree"
)

// These integration tests attach real disk images with hdiutil. They build a
// synthetic macOS tree once per test binary, package it as APFS and HFS+ DMGs
// and as raw images, and run every attach against those files. They are
// skipped in -short mode, off macOS, and when hdiutil is missing.

const (
	darwinOS = "darwin"

	// itHardwareModel is an Apple silicon model identifier (MacBook Air M2).
	itHardwareModel = "Mac14,2"

	// itTimezone is the zone the fixture's localtime symlink points at.
	itTimezone = "Europe/London"

	// itLocaltimeTarget is the absolute symlink target macOS writes.
	itLocaltimeTarget = "/var/db/timezone/zoneinfo/" + itTimezone

	// itZshHistory is alice's shell history in the fixture.
	itZshHistory = ": 1700000000:0;ls -la\n: 1700000010:0;cat /etc/hosts\n"

	// itTestTimeout bounds every hdiutil operation a test starts.
	itTestTimeout = 2 * time.Minute
)

// -----------------------------------------------------------------------------
// Fixture Construction
// -----------------------------------------------------------------------------

// imageFixtures are the disk images built once per test binary.
type imageFixtures struct {
	// dir holds every fixture file; all attachments made by these tests are
	// of images under it.
	dir string

	// tree is the synthetic macOS tree the images were built from.
	tree string

	// apfsDMG and hfsDMG are GPT-partitioned UDIF images (the hdiutil default
	// layout) with a single APFS or HFS+ volume.
	apfsDMG string
	hfsDMG  string

	// rawAPFS is an unpartitioned raw image of a bare APFS container.
	rawAPFS string

	// rawGPT is a raw image of a GPT-partitioned disk carrying an APFS
	// container, which is what dd of a whole disk produces.
	rawGPT string
}

var (
	fixturesOnce sync.Once
	fixtures     *imageFixtures
	errFixtures  error

	// fixturesDir is the root of the fixture files, removed by TestMain.
	fixturesDir string
)

// TestMain removes the shared fixture images after all tests have run, first
// force-detaching any attachment of them that a test leaked.
func TestMain(m *testing.M) {
	code := m.Run()

	if fixturesDir != "" {
		if leaked := detachLeaked(fixturesDir); len(leaked) > 0 {
			fmt.Fprintf(os.Stderr, "integration tests leaked attachments, force-detached: %v\n", leaked)
			code = 1
		}
		_ = os.RemoveAll(fixturesDir)
	}

	os.Exit(code)
}

// requireIntegration skips the test unless real hdiutil attaches can run.
func requireIntegration(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping disk image integration test in short mode")
	}
	if runtime.GOOS != darwinOS {
		t.Skip("disk image integration tests need hdiutil, which only exists on macOS")
	}
	if _, err := os.Stat(hdiutilPath); err != nil {
		t.Skipf("skipping: %s not available: %v", hdiutilPath, err)
	}
}

// getFixtures builds the fixture images on first use and returns them. It also
// registers the leaked-attachment guard on t, so every integration test that
// touches an image is covered by it.
func getFixtures(t *testing.T) *imageFixtures {
	t.Helper()
	requireIntegration(t)

	fixturesOnce.Do(func() {
		fixtures, errFixtures = buildFixtures()
	})
	if errFixtures != nil {
		t.Fatalf("building disk image fixtures: %v", errFixtures)
	}

	// Registered first, so it runs after every detach cleanup the test adds.
	t.Cleanup(func() {
		if leaked := detachLeaked(fixtures.dir); len(leaked) > 0 {
			t.Errorf("test left attachments behind; force-detached %v", leaked)
		}
	})
	return fixtures
}

// buildFixtures writes the synthetic tree and packages it as disk images. The
// slow hdiutil create runs happen concurrently.
func buildFixtures() (*imageFixtures, error) {
	dir, err := os.MkdirTemp("", "macfor-image-it-*")
	if err != nil {
		return nil, fmt.Errorf("create fixture dir: %w", err)
	}
	fixturesDir = dir

	f := &imageFixtures{
		dir:     dir,
		tree:    filepath.Join(dir, "tree"),
		apfsDMG: filepath.Join(dir, "apfs.dmg"),
		hfsDMG:  filepath.Join(dir, "hfs.dmg"),
		// Extensions hdiutil does not recognise, as on real evidence, so the
		// attach only succeeds through the CRawDiskImage image key.
		rawAPFS: filepath.Join(dir, "apfs-volume.dd"),
		rawGPT:  filepath.Join(dir, "gpt-disk.dd"),
	}

	if err := buildTree(f.tree); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), itTestTimeout)
	defer cancel()

	// The bare APFS volume is converted to raw from its own UDIF image.
	bareDMG := filepath.Join(dir, "apfs-bare.dmg")

	creates := [][]string{
		{"create", "-quiet", "-srcfolder", f.tree, "-fs", "APFS", "-volname", "MacforITAPFS", f.apfsDMG},
		{"create", "-quiet", "-srcfolder", f.tree, "-fs", "HFS+", "-volname", "MacforITHFS", f.hfsDMG},
		{"create", "-quiet", "-srcfolder", f.tree, "-fs", "APFS", "-layout", "NONE", "-volname", "MacforITRaw", bareDMG},
	}

	var wg sync.WaitGroup
	errs := make([]error, len(creates))
	for i, args := range creates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = runHdiutil(ctx, args...)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	// hdiutil create mounts each new image to copy the folder in, and has
	// been seen to return before that mount is gone, which makes the convert
	// below fail on a busy image. Let those mounts settle first.
	if err := settleAttachments(ctx, dir); err != nil {
		return nil, err
	}

	if err := convertToRaw(ctx, bareDMG, f.rawAPFS); err != nil {
		return nil, err
	}
	if err := convertToRaw(ctx, f.apfsDMG, f.rawGPT); err != nil {
		return nil, err
	}
	return f, nil
}

// buildTree writes a macostree tree plus the files that exercise hardware
// model, architecture, and time zone detection.
func buildTree(root string) error {
	opts := macostree.Defaults()
	opts.Users[0].ZshHistory = itZshHistory
	if err := macostree.Build(root, opts); err != nil {
		return fmt.Errorf("build macos tree: %w", err)
	}

	platformDir := filepath.Join(root, "private", "var", "db", "SystemConfiguration")
	if err := os.MkdirAll(platformDir, 0o755); err != nil { //nolint:gosec // Test fixture directory.
		return fmt.Errorf("create platform dir: %w", err)
	}
	platform, err := plist.MarshalIndent(map[string]string{"hardware-model": itHardwareModel}, plist.XMLFormat, "\t")
	if err != nil {
		return fmt.Errorf("marshal platform plist: %w", err)
	}
	if err := os.WriteFile(filepath.Join(platformDir, "com.apple.platform.plist"), platform, 0o600); err != nil {
		return fmt.Errorf("write platform plist: %w", err)
	}

	etcDir := filepath.Join(root, "private", "etc")
	if err := os.MkdirAll(etcDir, 0o755); err != nil { //nolint:gosec // Test fixture directory.
		return fmt.Errorf("create etc dir: %w", err)
	}
	if err := os.Symlink(itLocaltimeTarget, filepath.Join(etcDir, "localtime")); err != nil {
		return fmt.Errorf("create localtime symlink: %w", err)
	}
	return nil
}

// settleAttachments waits for hdiutil to release every image under dir,
// detaching any that is still attached after a grace period. It is fixture
// setup only: the attachments it clears are hdiutil create's, not the image
// source's.
func settleAttachments(ctx context.Context, dir string) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		images, err := attachedUnder(dir)
		if err != nil {
			return err
		}
		if len(images) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			for _, img := range images {
				if dev := topLevelDevNode(img.SystemEntities); dev != "" {
					_ = runHdiutil(ctx, "detach", "-force", dev)
				}
			}
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// convertToRaw writes a raw (UDTO) copy of the UDIF image src to dst.
func convertToRaw(ctx context.Context, src, dst string) error {
	// hdiutil convert -format UDTO appends .cdr to the output name.
	cdr := dst + ".cdr"

	// Retry briefly: convert fails while a just-created image is still busy.
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
			_ = os.Remove(cdr)
		}
		if err = runHdiutil(ctx, "convert", "-quiet", src, "-format", "UDTO", "-o", cdr); err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	if err := os.Rename(cdr, dst); err != nil {
		return fmt.Errorf("rename raw image: %w", err)
	}
	return nil
}

// runHdiutil runs hdiutil with args, discarding its output.
func runHdiutil(ctx context.Context, args ...string) error {
	_, err := execRunner{}.Run(ctx, hdiutilPath, args...)
	return err
}

// -----------------------------------------------------------------------------
// Attachment Inspection
// -----------------------------------------------------------------------------

// hdiutilImage is one entry of the images array printed by hdiutil info -plist.
type hdiutilImage struct {
	ImagePath      string          `plist:"image-path"`
	Writeable      bool            `plist:"writeable"`
	SystemEntities []hdiutilEntity `plist:"system-entities"`
}

// hdiutilInfoOutput is the top-level plist printed by hdiutil info -plist.
type hdiutilInfoOutput struct {
	Images []hdiutilImage `plist:"images"`
}

// attachedImages returns every image hdiutil currently has attached.
func attachedImages() ([]hdiutilImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	out, err := execRunner{}.Run(ctx, hdiutilPath, "info", "-plist")
	if err != nil {
		return nil, err
	}
	if i := strings.Index(string(out), "<?xml"); i > 0 {
		out = out[i:]
	}

	var info hdiutilInfoOutput
	if _, err := plist.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse hdiutil info: %w", err)
	}
	return info.Images, nil
}

// attachedUnder returns the attached images whose path lies under dir.
// hdiutil reports resolved paths (/private/var/...), so both forms of dir
// are matched.
func attachedUnder(dir string) ([]hdiutilImage, error) {
	images, err := attachedImages()
	if err != nil {
		return nil, err
	}

	prefixes := []string{dir + string(filepath.Separator)}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil && resolved != dir {
		prefixes = append(prefixes, resolved+string(filepath.Separator))
	}

	var found []hdiutilImage
	for _, img := range images {
		for _, p := range prefixes {
			if strings.HasPrefix(img.ImagePath, p) {
				found = append(found, img)
				break
			}
		}
	}
	return found, nil
}

// findAttached returns the hdiutil info entry for the image at path, if it is
// currently attached.
func findAttached(t *testing.T, path string) (hdiutilImage, bool) {
	t.Helper()

	images, err := attachedUnder(filepath.Dir(path))
	if err != nil {
		t.Fatalf("hdiutil info: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	for _, img := range images {
		if img.ImagePath == path || img.ImagePath == resolved {
			return img, true
		}
	}
	return hdiutilImage{}, false
}

// isAttached reports whether the image at path is currently attached.
func isAttached(t *testing.T, path string) bool {
	t.Helper()
	_, ok := findAttached(t, path)
	return ok
}

// assertAttachedReadOnly fails the test unless the image at path is attached
// and hdiutil reports the attachment as not writeable.
func assertAttachedReadOnly(t *testing.T, path string) {
	t.Helper()

	img, ok := findAttached(t, path)
	if !ok {
		t.Errorf("hdiutil info does not list %s while attached", path)
		return
	}
	if img.Writeable {
		t.Errorf("%s is attached writeable; evidence must be attached read-only", path)
	}
}

// detachLeaked force-detaches every attached image under dir and returns their
// paths. It is the last line of defence against a test leaking an attachment.
func detachLeaked(dir string) []string {
	images, err := attachedUnder(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "leak guard: hdiutil info failed: %v\n", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	leaked := make([]string, 0, len(images))
	for _, img := range images {
		leaked = append(leaked, img.ImagePath)
		if dev := topLevelDevNode(img.SystemEntities); dev != "" {
			if err := runHdiutil(ctx, "detach", "-force", dev); err != nil {
				fmt.Fprintf(os.Stderr, "leak guard: force-detach %s: %v\n", dev, err)
			}
		}
	}
	return leaked
}

// newImageSource opens path with New and registers its Close as a cleanup.
func newImageSource(t *testing.T, path string) *Source {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), itTestTimeout)
	defer cancel()

	src, err := New(ctx, path)
	if err != nil {
		t.Fatalf("New(%s): %v", path, err)
	}
	t.Cleanup(func() {
		if err := src.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return src
}

// dmgCases are the UDIF fixtures most tests run against.
func dmgCases(f *imageFixtures) []struct{ name, path string } {
	return []struct{ name, path string }{
		{"APFS", f.apfsDMG},
		{"HFS+", f.hfsDMG},
	}
}

// -----------------------------------------------------------------------------
// Attach and Detach Tests
// -----------------------------------------------------------------------------

func TestAttachDetachRoundTrip(t *testing.T) {
	f := getFixtures(t)

	for _, tc := range dmgCases(f) {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), itTestTimeout)
			defer cancel()

			a, err := attach(ctx, tc.path, FormatDMG)
			if err != nil {
				t.Fatalf("attach: %v", err)
			}
			t.Cleanup(func() { _ = a.cleanup() })

			if a.devNode == "" {
				t.Fatal("attachment recorded no device node")
			}
			if a.data != "" {
				t.Errorf("single-volume image classified with a Data volume %q", a.data)
			}

			mp := a.system
			b, err := os.ReadFile(filepath.Join(mp, systemVersionPlist))
			if err != nil {
				t.Fatalf("SystemVersion.plist not readable through mount point %s: %v", mp, err)
			}
			if !strings.Contains(string(b), "14.2.1") {
				t.Errorf("SystemVersion.plist content unexpected: %s", b)
			}
			assertAttachedReadOnly(t, tc.path)

			if err := a.detach(ctx); err != nil {
				t.Fatalf("detach: %v", err)
			}

			if _, err := os.Stat(filepath.Join(mp, systemVersionPlist)); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("mount point %s still serves files after detach (stat err = %v)", mp, err)
			}
			if isAttached(t, tc.path) {
				t.Errorf("hdiutil info still lists %s after detach", tc.path)
			}
		})
	}
}

func TestDetachIsIdempotent(t *testing.T) {
	f := getFixtures(t)

	ctx, cancel := context.WithTimeout(context.Background(), itTestTimeout)
	defer cancel()

	src, err := New(ctx, f.apfsDMG)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })

	if err := src.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if isAttached(t, f.apfsDMG) {
		t.Error("image still attached after Close")
	}
}

func TestNewWithCancelledContextAttachesNothing(t *testing.T) {
	f := getFixtures(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src, err := New(ctx, f.apfsDMG)
	if err == nil {
		_ = src.Close()
		t.Fatal("New with a cancelled context succeeded")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if isAttached(t, f.apfsDMG) {
		t.Error("image attached despite the cancelled context")
	}
}

// TestCancelDuringAttachLeavesNothingAttached cancels while hdiutil attach is
// running. A cancelled New must not leave the image attached.
//
// Killing hdiutil attach does not stop the attach, which the DiskImages daemon
// completes anyway, and loses the plist output naming the device to detach.
// attachWith therefore lets hdiutil finish and then detaches (see
// uncancellable). This regressed reliably on a warm attach (the image was
// attached recently), which this test arranges.
func TestCancelDuringAttachLeavesNothingAttached(t *testing.T) {
	f := getFixtures(t)

	// Warm up: a first attach makes the next one fast enough that a short
	// deadline lands after hdiutil has handed the attach to the daemon.
	warm := newImageSource(t, f.apfsDMG)
	if err := warm.Close(); err != nil {
		t.Fatalf("warm-up Close: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	src, err := New(ctx, f.apfsDMG)
	if err == nil {
		t.Log("attach finished before the deadline; closing")
		if err := src.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	} else {
		t.Logf("New failed as expected: %v", err)
	}

	// An attach the daemon completes after hdiutil was killed surfaces within
	// a few hundred milliseconds, so wait before checking.
	time.Sleep(1500 * time.Millisecond)

	img, ok := findAttached(t, f.apfsDMG)
	if !ok {
		return
	}
	var mounts []string
	for _, e := range img.SystemEntities {
		if e.MountPoint != "" {
			mounts = append(mounts, e.MountPoint)
		}
	}
	leaked := detachLeaked(f.dir)
	t.Errorf("cancelling during attach left the image attached and mounted at %v; force-detached %v",
		mounts, leaked)
}

func TestCloseAfterContextCancelled(t *testing.T) {
	f := getFixtures(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src, err := New(ctx, f.apfsDMG)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = src.Close() })

	// Collection was cancelled mid-flight: Close must still detach, because
	// it runs on its own context rather than the caller's.
	cancel()

	if err := src.Close(); err != nil {
		t.Fatalf("Close after cancel: %v", err)
	}
	if isAttached(t, f.apfsDMG) {
		t.Error("image still attached after Close")
	}
}

// -----------------------------------------------------------------------------
// Source Tests
// -----------------------------------------------------------------------------

func TestSourceFromDMG(t *testing.T) {
	f := getFixtures(t)

	for _, tc := range dmgCases(f) {
		t.Run(tc.name, func(t *testing.T) {
			src := newImageSource(t, tc.path)

			if src.Format() != FormatDMG {
				t.Errorf("Format() = %q, want %q", src.Format(), FormatDMG)
			}
			if src.ImagePath() != tc.path {
				t.Errorf("ImagePath() = %q, want %q", src.ImagePath(), tc.path)
			}
			if roots := src.Roots(); len(roots) != 1 || roots[0] != src.MountPoint() {
				t.Errorf("Roots() = %v, want [%s]", roots, src.MountPoint())
			}

			assertSourceReads(t, src)
		})
	}
}

// assertSourceReads checks Open, Stat, ReadDir, Walk, Glob, and Users through
// src against the fixture tree.
func assertSourceReads(t *testing.T, src *Source) {
	t.Helper()

	// Open.
	rc, err := src.Open("/Users/alice/.zsh_history")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read zsh_history: %v", err)
	}
	if string(b) != itZshHistory {
		t.Errorf("zsh_history = %q, want %q", b, itZshHistory)
	}

	// Stat.
	info, err := src.Stat("/private/etc/localtime")
	if err != nil {
		t.Fatalf("Stat localtime: %v", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("localtime mode = %v, want a symlink", info.Mode())
	}

	// ReadDir.
	entries, err := src.ReadDir("/Users")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	if !names["alice"] || !names["Shared"] {
		t.Errorf("ReadDir(/Users) = %v, want alice and Shared", names)
	}

	// Walk.
	var walked []string
	err = src.Walk("/Users/alice", func(path string, _ macfor.FileInfo, err error) error {
		if err != nil {
			return err
		}
		walked = append(walked, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(walked, "/Users/alice") || !contains(walked, "/Users/alice/.zsh_history") {
		t.Errorf("Walk(/Users/alice) visited %v, want the home dir and .zsh_history", walked)
	}

	// Glob.
	matches, err := src.Glob("/Users/*/.zsh_history")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 || matches[0] != "/Users/alice/.zsh_history" {
		t.Errorf("Glob = %v, want [/Users/alice/.zsh_history]", matches)
	}

	// Users.
	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	var alice *macfor.User
	for i := range users {
		if users[i].Username == "alice" {
			alice = &users[i]
		}
	}
	if alice == nil {
		t.Fatalf("Users() = %+v, want alice", users)
	}
	if alice.UID != 501 {
		t.Errorf("alice UID = %d, want 501", alice.UID)
	}
	if got := src.ResolvePath("~alice/.zsh_history"); got == "" {
		t.Error("ResolvePath returned an empty path")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// System Info Tests
// -----------------------------------------------------------------------------

func TestSystemInfoFromDMG(t *testing.T) {
	f := getFixtures(t)

	// The directory source over the tree the image was built from is the
	// oracle: an attached image must report the same system.
	treeSrc, err := directory.New(f.tree)
	if err != nil {
		t.Fatalf("directory.New: %v", err)
	}
	want, err := treeSrc.SystemInfo()
	if err != nil {
		t.Fatalf("directory SystemInfo: %v", err)
	}
	opts := macostree.Defaults()

	for _, tc := range dmgCases(f) {
		t.Run(tc.name, func(t *testing.T) {
			src := newImageSource(t, tc.path)

			got, err := src.SystemInfo()
			if err != nil {
				t.Fatalf("SystemInfo: %v", err)
			}

			checks := []struct{ field, got, want string }{
				{"OSVersion", got.OSVersion, opts.OSVersion},
				{"OSBuild", got.OSBuild, opts.OSBuild},
				{"Hostname", got.Hostname, want.Hostname},
				{"ComputerName", got.ComputerName, opts.ComputerName},
				{"LocalHostname", got.LocalHostname, opts.LocalHostname},
				{"HardwareModel", got.HardwareModel, itHardwareModel},
				{"HardwareModelName", got.HardwareModelName, want.HardwareModelName},
				{"Architecture", got.Architecture, "arm64"},
				{"Timezone", got.Timezone, itTimezone},
				{"SourceType", string(got.SourceType), string(macfor.SourceTypeImage)},
				{"ImagePath", got.ImagePath, tc.path},
				{"ImageFormat", got.ImageFormat, "dmg"},
			}
			for _, c := range checks {
				if c.got != c.want {
					t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
				}
			}

			if got.Hostname == "" || got.Hostname == "unknown" {
				t.Errorf("Hostname = %q, want the fixture hostname", got.Hostname)
			}
			if strings.HasPrefix(got.ImagePath, src.MountPoint()) {
				t.Errorf("ImagePath %q is the mount point, not the image file", got.ImagePath)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Raw Image Tests
// -----------------------------------------------------------------------------

func TestRawVolumeImage(t *testing.T) {
	f := getFixtures(t)

	format, err := DetectFormat(f.rawAPFS)
	if err != nil {
		t.Fatalf("DetectFormat: %v", err)
	}
	if format != FormatRaw {
		t.Fatalf("DetectFormat = %q, want %q", format, FormatRaw)
	}

	src := newImageSource(t, f.rawAPFS)

	if src.Format() != FormatRaw {
		t.Errorf("Format() = %q, want %q", src.Format(), FormatRaw)
	}
	// The .dd extension means only the CRawDiskImage image key lets hdiutil
	// attach it at all; image-type reports the image class, not the mode.
	assertAttachedReadOnly(t, f.rawAPFS)

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo: %v", err)
	}
	if info.ImageFormat != "raw" {
		t.Errorf("ImageFormat = %q, want raw", info.ImageFormat)
	}
	if info.OSVersion != "14.2.1" {
		t.Errorf("OSVersion = %q, want 14.2.1", info.OSVersion)
	}

	assertSourceReads(t, src)
}

// TestRawWholeDiskImage covers a raw image of a GPT-partitioned disk, the
// shape dd of a whole Mac disk produces. hdiutil attaches it with the
// CRawDiskImage image key; DetectFormat, however, only recognises filesystem
// magics at volume offsets (APFS NXSB at 0x20, HFS+ at 0x400), which an
// unpartitioned volume has but a partitioned disk does not: its first sectors
// hold a protective MBR and the "EFI PART" GPT header at 0x200.
func TestRawWholeDiskImage(t *testing.T) {
	f := getFixtures(t)

	ctx, cancel := context.WithTimeout(context.Background(), itTestTimeout)
	defer cancel()

	// The attach itself works when the format is supplied.
	a, err := attach(ctx, f.rawGPT, FormatRaw)
	if err != nil {
		t.Fatalf("attach raw GPT disk image: %v", err)
	}
	t.Cleanup(func() { _ = a.cleanup() })

	if _, err := os.Stat(filepath.Join(a.system, systemVersionPlist)); err != nil {
		t.Errorf("SystemVersion.plist not readable on raw GPT image: %v", err)
	}
	if err := a.detach(ctx); err != nil {
		t.Fatalf("detach: %v", err)
	}

	format, err := DetectFormat(f.rawGPT)
	if format != FormatRaw {
		t.Fatalf("DetectFormat(raw GPT disk image) = %q, %v; want raw", format, err)
	}

	src := newImageSource(t, f.rawGPT)
	assertSourceReads(t, src)
}

// -----------------------------------------------------------------------------
// E01 Tests
// -----------------------------------------------------------------------------

func TestE01Image(t *testing.T) {
	f := getFixtures(t)

	if _, err := exec.LookPath(ewfmountName); err != nil {
		t.Skip("skipping E01 integration test: ewfmount not found on PATH (brew install libewf, plus macFUSE)")
	}
	acquire, err := exec.LookPath("ewfacquirestream")
	if err != nil {
		t.Skip("skipping E01 integration test: ewfacquirestream not found on PATH, cannot build an E01 fixture")
	}

	ctx, cancel := context.WithTimeout(context.Background(), itTestTimeout)
	defer cancel()

	// Wrap the raw volume image as an E01 by streaming it through
	// ewfacquirestream, which reads the source data from stdin.
	in, err := os.Open(f.rawAPFS)
	if err != nil {
		t.Fatalf("open raw image: %v", err)
	}
	defer func() { _ = in.Close() }()

	target := filepath.Join(f.dir, "evidence")
	cmd := exec.CommandContext(ctx, acquire, "-q", "-t", target) //nolint:gosec // Fixed tool resolved on PATH in a test.
	cmd.Stdin = in
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ewfacquirestream: %v: %s", err, out)
	}
	e01 := target + ".E01"

	format, err := DetectFormat(e01)
	if err != nil || format != FormatE01 {
		t.Fatalf("DetectFormat = %q, %v; want %q", format, err, FormatE01)
	}

	src := newImageSource(t, e01)
	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo: %v", err)
	}
	if info.ImageFormat != "e01" || info.ImagePath != e01 {
		t.Errorf("provenance = %q %q, want e01 %q", info.ImageFormat, info.ImagePath, e01)
	}
	assertSourceReads(t, src)
}
