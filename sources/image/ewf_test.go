package image

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	// fakeEWFMountPath is where the fake lookup claims ewfmount lives.
	fakeEWFMountPath = "/opt/homebrew/bin/ewfmount"

	// e01Image is the first segment of a fake segmented E01 set.
	e01Image = "/evidence/case-42/mac.E01"
)

// ewfHarness runs the E01 lifecycle against fakes, recording every command and
// removal in a single ordered log on its fakeRunner.
type ewfHarness struct {
	t      *testing.T
	runner *fakeRunner
	tools  ewfTools

	// attachOut is the hdiutil attach -plist output to return.
	attachOut []byte

	// Failure switches, read when the matching command runs.
	failEWFMount  bool
	noDevice      bool
	failAttach    bool
	failDetach    bool
	failUmount    bool
	failUmountF   bool
	cancelOnMount context.CancelFunc
}

func newEWFHarness(t *testing.T) *ewfHarness {
	t.Helper()

	volumes := t.TempDir()
	mkTree(t, filepath.Join(volumes, "Macintosh HD"), systemVersionPlist)

	h := &ewfHarness{t: t, attachOut: loadFixture(t, singleFixture, volumes)}
	h.runner = &fakeRunner{respond: h.respond}
	h.tools = ewfTools{
		runner:    h.runner,
		lookPath:  func(string) (string, error) { return fakeEWFMountPath, nil },
		removeAll: h.removeAll,
		tempDir:   t.TempDir(),
	}
	return h
}

// respond scripts each external tool by recognising its arguments.
func (h *ewfHarness) respond(_ context.Context, _ int, args []string) ([]byte, error) {
	switch {
	case args[0] == "attach":
		if h.failAttach {
			return nil, errors.New("hdiutil attach: exit status 1: attach failed - no mountable file systems")
		}
		return h.attachOut, nil
	case args[0] == "detach":
		if h.failDetach {
			return nil, errors.New("hdiutil detach: exit status 16: Resource busy")
		}
		return nil, nil
	case args[0] == "-f":
		if h.failUmountF {
			return nil, errors.New("umount -f: exit status 1: Resource busy")
		}
		return nil, nil
	case len(args) == 2 && args[0] == e01Image:
		if h.failEWFMount {
			return nil, errors.New("ewfmount: exit status 1: unable to open source file(s)")
		}
		if h.cancelOnMount != nil {
			h.cancelOnMount()
		}
		if !h.noDevice {
			// Stand in for the FUSE mount by exposing the raw device file.
			if err := os.WriteFile(filepath.Join(args[1], ewfDeviceName), nil, 0o600); err != nil {
				h.t.Fatalf("write device: %v", err)
			}
		}
		return nil, nil
	default:
		if h.failUmount {
			return nil, errors.New("umount: exit status 1: Resource busy")
		}
		return nil, nil
	}
}

// removeAll records the removal in the command log, then really removes.
func (h *ewfHarness) removeAll(path string) error {
	h.runner.mu.Lock()
	h.runner.calls = append(h.runner.calls, fakeCall{name: "RemoveAll", args: []string{path}})
	h.runner.mu.Unlock()
	return os.RemoveAll(path)
}

// mountDir returns the single mount dir created under the harness temp dir, or
// empty when none remains.
func (h *ewfHarness) mountDir() string {
	h.t.Helper()

	entries, err := os.ReadDir(h.tools.tempDir)
	if err != nil {
		h.t.Fatalf("read temp dir: %v", err)
	}
	if len(entries) == 0 {
		return ""
	}
	if len(entries) > 1 {
		h.t.Fatalf("got %d mount dirs, want 1", len(entries))
	}
	return filepath.Join(h.tools.tempDir, entries[0].Name())
}

// ewfDirArg returns the mount dir ewfmount was given, from the recorded calls.
func (h *ewfHarness) ewfDirArg() string {
	h.t.Helper()

	for _, c := range h.runner.calls {
		if c.name == fakeEWFMountPath {
			return c.args[1]
		}
	}
	h.t.Fatal("ewfmount never ran")
	return ""
}

func assertCommands(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands:\n got  %q\n want %q", got, want)
	}
}

func TestMountEWF(t *testing.T) {
	t.Run("missing binary names tool and remedy", func(t *testing.T) {
		h := newEWFHarness(t)
		h.tools.lookPath = func(file string) (string, error) {
			return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
		}

		_, err := mountEWF(context.Background(), h.tools, e01Image)
		if !errors.Is(err, ErrEWFMountNotFound) || !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("err = %v, want ErrEWFMountNotFound wrapping exec.ErrNotFound", err)
		}
		if msg := err.Error(); !strings.Contains(msg, "ewfmount") || !strings.Contains(msg, "brew install libewf") {
			t.Errorf("err = %q, want tool name and remedy", msg)
		}
		if len(h.runner.commands()) != 0 {
			t.Errorf("commands = %q, want none", h.runner.commands())
		}
		if dir := h.mountDir(); dir != "" {
			t.Errorf("mount dir %s created without ewfmount", dir)
		}
	})

	t.Run("success mounts first segment onto private dir", func(t *testing.T) {
		h := newEWFHarness(t)

		m, err := mountEWF(context.Background(), h.tools, e01Image)
		if err != nil {
			t.Fatalf("mountEWF: %v", err)
		}

		dir := h.mountDir()
		if m.dir != dir {
			t.Errorf("dir = %q, want %q", m.dir, dir)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat mount dir: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("mount dir mode = %o, want 700", perm)
		}
		if got, want := m.device(), filepath.Join(dir, "ewf1"); got != want {
			t.Errorf("device = %q, want %q", got, want)
		}

		// ewfmount runs by its resolved path, given only the first segment.
		if c := h.runner.calls[0]; c.name != fakeEWFMountPath || !reflect.DeepEqual(c.args, []string{e01Image, dir}) {
			t.Errorf("ran %q %q", c.name, c.args)
		}
	})

	t.Run("ewfmount failure removes dir only", func(t *testing.T) {
		h := newEWFHarness(t)
		h.failEWFMount = true

		_, err := mountEWF(context.Background(), h.tools, e01Image)
		if err == nil || !strings.Contains(err.Error(), "unable to open source") {
			t.Fatalf("err = %v, want ewfmount stderr", err)
		}
		dir := h.ewfDirArg()
		assertCommands(t, h.runner.commands(), []string{
			"ewfmount " + e01Image + " " + dir,
			"RemoveAll " + dir,
		})
		if left := h.mountDir(); left != "" {
			t.Errorf("mount dir %s leaked", left)
		}
	})

	t.Run("missing device unmounts then removes", func(t *testing.T) {
		h := newEWFHarness(t)
		h.noDevice = true

		_, err := mountEWF(context.Background(), h.tools, e01Image)
		if err == nil || !strings.Contains(err.Error(), "ewf1") {
			t.Fatalf("err = %v, want missing device error", err)
		}
		dir := h.ewfDirArg()
		assertCommands(t, h.runner.commands(), []string{
			"ewfmount " + e01Image + " " + dir,
			"umount " + dir,
			"RemoveAll " + dir,
		})
	})

	t.Run("cancelled during ewfmount unmounts on fresh context", func(t *testing.T) {
		h := newEWFHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h.cancelOnMount = cancel

		var umountCtxErr error
		inner := h.runner.respond
		h.runner.respond = func(c context.Context, n int, args []string) ([]byte, error) {
			if n == 1 {
				umountCtxErr = c.Err()
			}
			return inner(c, n, args)
		}

		m, err := mountEWF(ctx, h.tools, e01Image)
		if !errors.Is(err, context.Canceled) || m != nil {
			t.Fatalf("mountEWF = %v, %v; want nil, context.Canceled", m, err)
		}
		dir := h.ewfDirArg()
		assertCommands(t, h.runner.commands(), []string{
			"ewfmount " + e01Image + " " + dir,
			"umount " + dir,
			"RemoveAll " + dir,
		})
		if umountCtxErr != nil {
			t.Errorf("cleanup umount ran on a done context: %v", umountCtxErr)
		}
	})

	t.Run("already cancelled runs nothing", func(t *testing.T) {
		h := newEWFHarness(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := mountEWF(ctx, h.tools, e01Image); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if len(h.runner.commands()) != 0 || h.mountDir() != "" {
			t.Errorf("commands = %q, dir = %q; want nothing", h.runner.commands(), h.mountDir())
		}
	})
}

func TestEWFUnmount(t *testing.T) {
	t.Run("idempotent", func(t *testing.T) {
		h := newEWFHarness(t)
		m, err := mountEWF(context.Background(), h.tools, e01Image)
		if err != nil {
			t.Fatalf("mountEWF: %v", err)
		}

		for i := range 2 {
			if err := m.unmount(context.Background()); err != nil {
				t.Fatalf("unmount #%d: %v", i+1, err)
			}
		}
		assertCommands(t, h.runner.commands(), []string{
			"ewfmount " + e01Image + " " + m.dir,
			"umount " + m.dir,
			"RemoveAll " + m.dir,
		})
		if left := h.mountDir(); left != "" {
			t.Errorf("mount dir %s left behind", left)
		}
	})

	t.Run("busy retried with force", func(t *testing.T) {
		h := newEWFHarness(t)
		m, err := mountEWF(context.Background(), h.tools, e01Image)
		if err != nil {
			t.Fatalf("mountEWF: %v", err)
		}
		h.failUmount = true

		if err := m.unmount(context.Background()); err != nil {
			t.Fatalf("unmount: %v", err)
		}
		assertCommands(t, h.runner.commands()[1:], []string{
			"umount " + m.dir,
			"umount -f " + m.dir,
			"RemoveAll " + m.dir,
		})
	})

	t.Run("failed umount keeps dir and can be retried", func(t *testing.T) {
		h := newEWFHarness(t)
		m, err := mountEWF(context.Background(), h.tools, e01Image)
		if err != nil {
			t.Fatalf("mountEWF: %v", err)
		}
		h.failUmount, h.failUmountF = true, true

		err = m.unmount(context.Background())
		if err == nil || !strings.Contains(err.Error(), "forced retry") {
			t.Fatalf("err = %v, want both attempts", err)
		}
		if h.mountDir() == "" {
			t.Fatal("mount dir removed while still mounted")
		}

		h.failUmount, h.failUmountF = false, false
		if err := m.unmount(context.Background()); err != nil {
			t.Fatalf("retry unmount: %v", err)
		}
		assertCommands(t, h.runner.commands()[1:], []string{
			"umount " + m.dir,
			"umount -f " + m.dir,
			"umount " + m.dir,
			"RemoveAll " + m.dir,
		})
	})
}

func TestAttachEWFWith(t *testing.T) {
	t.Run("teardown runs in strict reverse order", func(t *testing.T) {
		h := newEWFHarness(t)

		a, err := attachEWFWith(context.Background(), h.tools, e01Image)
		if err != nil {
			t.Fatalf("attachEWFWith: %v", err)
		}
		if a.ewf == nil || !a.mounted() {
			t.Fatalf("attachment = %+v, want mounted with ewf", a)
		}

		for i := range 2 {
			if err := a.detach(context.Background()); err != nil {
				t.Fatalf("detach #%d: %v", i+1, err)
			}
		}

		dir := h.ewfDirArg()
		assertCommands(t, h.runner.commands(), []string{
			"ewfmount " + e01Image + " " + dir,
			"hdiutil attach -readonly -nobrowse -plist -imagekey diskimage-class=CRawDiskImage " + filepath.Join(dir, "ewf1"),
			"hdiutil detach /dev/disk4",
			"umount " + dir,
			"RemoveAll " + dir,
		})
		if left := h.mountDir(); left != "" {
			t.Errorf("mount dir %s left behind", left)
		}
	})

	t.Run("hdiutil attach failure unwinds ewf mount", func(t *testing.T) {
		h := newEWFHarness(t)
		h.failAttach = true

		a, err := attachEWFWith(context.Background(), h.tools, e01Image)
		if err == nil || a != nil || !strings.Contains(err.Error(), "no mountable file systems") {
			t.Fatalf("attachEWFWith = %v, %v; want nil and hdiutil stderr", a, err)
		}

		dir := h.ewfDirArg()
		want := []string{
			"ewfmount " + e01Image + " " + dir,
			"hdiutil attach -readonly -nobrowse -plist -imagekey diskimage-class=CRawDiskImage " + filepath.Join(dir, "ewf1"),
			"umount " + dir,
			"RemoveAll " + dir,
		}
		assertCommands(t, h.runner.commands(), want)
		if left := h.mountDir(); left != "" {
			t.Errorf("mount dir %s leaked", left)
		}
	})

	t.Run("missing ewfmount runs nothing", func(t *testing.T) {
		h := newEWFHarness(t)
		h.tools.lookPath = func(file string) (string, error) {
			return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
		}

		if _, err := attachEWFWith(context.Background(), h.tools, "/evidence/other/disk.Ex01"); !errors.Is(err, ErrEWFMountNotFound) {
			t.Fatalf("err = %v, want ErrEWFMountNotFound", err)
		}
		if len(h.runner.commands()) != 0 {
			t.Errorf("commands = %q, want none", h.runner.commands())
		}
	})

	t.Run("failed hdiutil detach leaves ewf mount until retried", func(t *testing.T) {
		h := newEWFHarness(t)
		a, err := attachEWFWith(context.Background(), h.tools, e01Image)
		if err != nil {
			t.Fatalf("attachEWFWith: %v", err)
		}

		h.failDetach = true
		if err := a.detach(context.Background()); err == nil {
			t.Fatal("want detach error")
		}
		for _, c := range h.runner.commands() {
			if strings.HasPrefix(c, "umount") || strings.HasPrefix(c, "RemoveAll") {
				t.Fatalf("ewf mount torn down before hdiutil detach succeeded: %q", h.runner.commands())
			}
		}

		h.failDetach = false
		if err := a.detach(context.Background()); err != nil {
			t.Fatalf("retry detach: %v", err)
		}
		dir := h.ewfDirArg()
		assertCommands(t, h.runner.commands()[2:], []string{
			"hdiutil detach /dev/disk4",
			"hdiutil detach -force /dev/disk4",
			"hdiutil detach /dev/disk4",
			"umount " + dir,
			"RemoveAll " + dir,
		})
	})

	t.Run("failed umount after detach is retried without re-detaching", func(t *testing.T) {
		h := newEWFHarness(t)
		a, err := attachEWFWith(context.Background(), h.tools, e01Image)
		if err != nil {
			t.Fatalf("attachEWFWith: %v", err)
		}

		h.failUmount, h.failUmountF = true, true
		if err := a.detach(context.Background()); err == nil {
			t.Fatal("want umount error")
		}
		h.failUmount, h.failUmountF = false, false
		if err := a.detach(context.Background()); err != nil {
			t.Fatalf("retry detach: %v", err)
		}

		dir := h.ewfDirArg()
		assertCommands(t, h.runner.commands()[2:], []string{
			"hdiutil detach /dev/disk4",
			"umount " + dir,
			"umount -f " + dir,
			"umount " + dir,
			"RemoveAll " + dir,
		})
	})
}
