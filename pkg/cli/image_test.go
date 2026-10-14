package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/image"
)

// fakeImageSource is an imageSource that never attaches anything. It records
// how often Close was called and can be told to fail it.
type fakeImageSource struct {
	mockSource

	path     string
	closeErr error

	mu         sync.Mutex
	closeCalls int
}

func (f *fakeImageSource) Type() macfor.SourceType { return macfor.SourceTypeImage }
func (f *fakeImageSource) ImagePath() string       { return f.path }
func (f *fakeImageSource) Format() image.Format    { return image.FormatDMG }
func (f *fakeImageSource) MountPoint() string      { return "/Volumes/fake-system" }
func (f *fakeImageSource) Roots() []string {
	return []string{"/Volumes/fake-data", "/Volumes/fake-system"}
}

func (f *fakeImageSource) SystemInfo() (*macfor.SystemInfo, error) {
	return &macfor.SystemInfo{
		OSName:      "macOS",
		OSVersion:   "14.0",
		Hostname:    "imaged-host",
		SourceType:  macfor.SourceTypeImage,
		ImagePath:   f.path,
		ImageFormat: string(image.FormatDMG),
	}, nil
}

func (f *fakeImageSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalls++
	return f.closeErr
}

func (f *fakeImageSource) closes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closeCalls
}

// installFakeImageSource replaces newImageSource for the test with one that
// returns fake (or attachErr) and counts calls, and creates a regular file to
// pass as the image path.
func installFakeImageSource(t *testing.T, fake *fakeImageSource, attachErr error) (imagePath string, attaches *int) {
	t.Helper()

	imagePath = filepath.Join(t.TempDir(), "suspect.dmg")
	if err := os.WriteFile(imagePath, []byte("not really an image"), 0o600); err != nil {
		t.Fatalf("writing image file: %v", err)
	}
	if fake != nil {
		fake.path = imagePath
	}

	calls := 0
	orig := newImageSource
	newImageSource = func(ctx context.Context, path string) (imageSource, error) {
		calls++
		if ctx == nil {
			t.Error("newImageSource called with nil context")
		}
		if path != imagePath {
			t.Errorf("newImageSource path = %q, want %q", path, imagePath)
		}
		if attachErr != nil {
			return nil, attachErr
		}
		return fake, nil
	}
	t.Cleanup(func() { newImageSource = orig })
	return imagePath, &calls
}

// forbidImageAttach makes any attach attempt fail the test.
func forbidImageAttach(t *testing.T) {
	t.Helper()
	orig := newImageSource
	newImageSource = func(_ context.Context, path string) (imageSource, error) {
		t.Errorf("unexpected image attach of %s", path)
		return nil, errors.New("attach forbidden in test")
	}
	t.Cleanup(func() { newImageSource = orig })
}

// runCollectArgs runs the collect command with args and returns its output.
func runCollectArgs(ctx context.Context, t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(append([]string{"collect"}, args...))
	err := rootCmd.ExecuteContext(ctx)
	return buf.String(), err
}

// readContainerJSON decodes the named JSON file from an evidence container.
func readContainerJSON(t *testing.T, containerPath, name string, v any) {
	t.Helper()
	zr, err := zip.OpenReader(containerPath)
	if err != nil {
		t.Fatalf("opening container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening %s: %v", name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatalf("decoding %s: %v", name, err)
		}
		return
	}
	t.Fatalf("container has no %s", name)
}

// custodyActions returns the custody log's actions in order.
func custodyActions(log container.ChainOfCustodyLog) []string {
	actions := make([]string, 0, len(log.Entries))
	for _, e := range log.Entries {
		actions = append(actions, e.Action)
	}
	return actions
}

// indexOf returns the index of the first occurrence of s in list, or -1.
func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

func TestRunCollect_ImageAndFromMutuallyExclusive(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	cleanup := registerCollectTestPlugins()
	defer cleanup()
	forbidImageAttach(t)

	_, err := runCollectArgs(context.Background(), t,
		"--image", "/tmp/whatever.dmg", "--from", t.TempDir(), "--dry-run")
	if !errors.Is(err, errImageAndFrom) {
		t.Fatalf("expected errImageAndFrom, got: %v", err)
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should say mutually exclusive, got: %v", err)
	}
}

func TestCreateSource_ImageAndFromMutuallyExclusive(t *testing.T) {
	forbidImageAttach(t)

	_, err := createSource(context.Background(), t.TempDir(), "/tmp/whatever.dmg")
	if !errors.Is(err, errImageAndFrom) {
		t.Fatalf("expected errImageAndFrom, got: %v", err)
	}
}

func TestRunCollect_ImageNonExistentPath(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	cleanup := registerCollectTestPlugins()
	defer cleanup()
	forbidImageAttach(t)

	_, err := runCollectArgs(context.Background(), t,
		"--image", "/nonexistent/path/suspect.dmg", "--dry-run")
	if err == nil {
		t.Fatal("expected error for nonexistent --image path")
	}
	if !strings.Contains(err.Error(), "image file does not exist") {
		t.Errorf("expected 'image file does not exist' error, got: %v", err)
	}
}

func TestRunCollect_ImageIsDirectory(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	cleanup := registerCollectTestPlugins()
	defer cleanup()
	forbidImageAttach(t)

	_, err := runCollectArgs(context.Background(), t, "--image", t.TempDir(), "--dry-run")
	if err == nil {
		t.Fatal("expected error for directory passed to --image")
	}
	if !strings.Contains(err.Error(), "image path is a directory") {
		t.Errorf("expected 'image path is a directory' error, got: %v", err)
	}
}

func TestCreateSource_Image(t *testing.T) {
	fake := &fakeImageSource{}
	imagePath, attaches := installFakeImageSource(t, fake, nil)

	src, err := createSource(context.Background(), "", imagePath)
	if err != nil {
		t.Fatalf("createSource: %v", err)
	}
	if *attaches != 1 {
		t.Errorf("attaches = %d, want 1", *attaches)
	}
	if src != fake {
		t.Errorf("createSource returned %T, want the fake image source", src)
	}
	if _, ok := src.(io.Closer); !ok {
		t.Error("image source should implement io.Closer")
	}
}

func TestCreateSource_ImageAttachError(t *testing.T) {
	attachErr := errors.New("hdiutil: attach failed")
	imagePath, _ := installFakeImageSource(t, nil, attachErr)

	src, err := createSource(context.Background(), "", imagePath)
	if src != nil {
		t.Errorf("expected nil source on attach error, got %T", src)
	}
	if !errors.Is(err, attachErr) {
		t.Fatalf("expected wrapped attach error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "creating image source") {
		t.Errorf("expected 'creating image source' context, got: %v", err)
	}
}

func TestRunCollect_ImageDryRun(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	fake := &fakeImageSource{}
	imagePath, _ := installFakeImageSource(t, fake, nil)

	output, err := runCollectArgs(context.Background(), t, "--image", imagePath, "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Source: image (" + imagePath + ", dmg)"
	if !strings.Contains(output, want) {
		t.Errorf("expected %q in output, got: %s", want, output)
	}
	if got := fake.closes(); got != 1 {
		t.Errorf("Close calls = %d, want 1 (dry run must still detach)", got)
	}
}

func TestRunCollect_ImageCollection(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	fake := &fakeImageSource{}
	imagePath, _ := installFakeImageSource(t, fake, nil)

	collected := false
	macfor.Register(&collectMockPlugin{
		info:         macfor.PluginInfo{ID: "test.collect", Name: "Collect Test", Version: "1.0.0", Categories: []string{"test"}},
		detectResult: macfor.DetectionResult{Applicable: true, Reason: "always applicable"},
		collectCallback: func(_ context.Context, _ macfor.Source, _ macfor.Collector) error {
			collected = true
			if fake.closes() != 0 {
				t.Error("image detached before plugins collected")
			}
			return nil
		},
	})

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	if _, err := runCollectArgs(context.Background(), t, "--image", imagePath, "--output", outputPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !collected {
		t.Fatal("plugin did not collect")
	}
	if got := fake.closes(); got != 1 {
		t.Errorf("Close calls = %d, want exactly 1", got)
	}

	var manifest container.Manifest
	readContainerJSON(t, outputPath, "manifest.json", &manifest)
	if manifest.Source.Type != string(macfor.SourceTypeImage) {
		t.Errorf("source.type = %q, want image", manifest.Source.Type)
	}
	if manifest.Source.ImagePath != imagePath {
		t.Errorf("source.image_path = %q, want %q", manifest.Source.ImagePath, imagePath)
	}
	if manifest.Source.ImageFormat != "dmg" {
		t.Errorf("source.image_format = %q, want dmg", manifest.Source.ImageFormat)
	}

	var custody container.ChainOfCustodyLog
	readContainerJSON(t, outputPath, "chain-of-custody.json", &custody)
	actions := custodyActions(custody)

	started := indexOf(actions, container.CustodyActionCollectionStarted)
	attached := indexOf(actions, container.CustodyActionImageAttached)
	pluginStarted := indexOf(actions, container.CustodyActionPluginStarted)
	pluginDone := indexOf(actions, container.CustodyActionPluginCompleted)
	detached := indexOf(actions, container.CustodyActionImageDetached)
	completed := indexOf(actions, container.CustodyActionCollectionCompleted)

	if started < 0 || attached < 0 || pluginStarted < 0 || pluginDone < 0 || detached < 0 || completed < 0 {
		t.Fatalf("custody log missing an expected action: %v", actions)
	}
	order := []int{started, attached, pluginStarted, pluginDone, detached, completed}
	for i := 1; i < len(order); i++ {
		if order[i-1] >= order[i] {
			t.Errorf("custody actions out of order: %v", actions)
			break
		}
	}
	if indexOf(actions, container.CustodyActionImageDetachFailed) >= 0 {
		t.Errorf("unexpected detach failure in custody log: %v", actions)
	}

	att := custody.Entries[attached]
	if att.Target != imagePath {
		t.Errorf("image_attached target = %q, want %q", att.Target, imagePath)
	}
	if att.Details["image_format"] != "dmg" {
		t.Errorf("image_attached image_format = %v, want dmg", att.Details["image_format"])
	}
	if att.Details["mount_point"] != "/Volumes/fake-system" {
		t.Errorf("image_attached mount_point = %v", att.Details["mount_point"])
	}
	if roots, ok := att.Details["roots"].([]any); !ok || len(roots) != 2 {
		t.Errorf("image_attached roots = %v, want 2 roots", att.Details["roots"])
	}
	if att.Timestamp.After(custody.Entries[detached].Timestamp) {
		t.Error("image_attached timestamp is after image_detached")
	}
}

func TestRunCollect_ImageDetachFailureRecordedNotFatal(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	fake := &fakeImageSource{closeErr: errors.New("hdiutil: resource busy")}
	imagePath, _ := installFakeImageSource(t, fake, nil)

	macfor.Register(&collectMockPlugin{
		info:         macfor.PluginInfo{ID: "test.collect", Name: "Collect Test", Version: "1.0.0", Categories: []string{"test"}},
		detectResult: macfor.DetectionResult{Applicable: true, Reason: "always applicable"},
	})

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	if _, err := runCollectArgs(context.Background(), t, "--image", imagePath, "--output", outputPath); err != nil {
		t.Fatalf("detach failure must not fail the collection, got: %v", err)
	}

	// One attempt before finalize, one retry from the deferred safety net.
	if got := fake.closes(); got != 2 {
		t.Errorf("Close calls = %d, want 2 (pre-finalize attempt + deferred retry)", got)
	}

	var custody container.ChainOfCustodyLog
	readContainerJSON(t, outputPath, "chain-of-custody.json", &custody)
	actions := custodyActions(custody)
	failed := indexOf(actions, container.CustodyActionImageDetachFailed)
	if failed < 0 {
		t.Fatalf("custody log missing image_detach_failed: %v", actions)
	}
	if indexOf(actions, container.CustodyActionImageDetached) >= 0 {
		t.Errorf("custody log should not record a successful detach: %v", actions)
	}
	if !strings.Contains(custody.Entries[failed].Details["error"].(string), "resource busy") {
		t.Errorf("image_detach_failed error = %v", custody.Entries[failed].Details["error"])
	}
}

func TestRunCollect_ImageClosedOnErrorPath(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	// Detach also fails: the collection's own error must still be returned.
	fake := &fakeImageSource{closeErr: errors.New("hdiutil: resource busy")}
	imagePath, _ := installFakeImageSource(t, fake, nil)

	_, err := runCollectArgs(context.Background(), t,
		"--image", imagePath, "--plugin", "no.such.plugin", "--dry-run")
	if err == nil {
		t.Fatal("expected error for unknown plugin")
	}
	if !strings.Contains(err.Error(), "plugin not found") {
		t.Errorf("detach failure masked the collection error, got: %v", err)
	}
	if got := fake.closes(); got != 1 {
		t.Errorf("Close calls = %d, want 1 on the error path", got)
	}
}

func TestRunCollect_ImageClosedOnCancel(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	fake := &fakeImageSource{}
	imagePath, _ := installFakeImageSource(t, fake, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	macfor.Register(&collectMockPlugin{
		info:         macfor.PluginInfo{ID: "test.collect", Name: "Collect Test", Version: "1.0.0", Categories: []string{"test"}},
		detectResult: macfor.DetectionResult{Applicable: true, Reason: "always applicable"},
		collectCallback: func(_ context.Context, _ macfor.Source, _ macfor.Collector) error {
			cancel() // Simulates SIGINT arriving mid-collection.
			return nil
		},
	})

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	_, _ = runCollectArgs(ctx, t, "--image", imagePath, "--output", outputPath)

	if got := fake.closes(); got != 1 {
		t.Errorf("Close calls = %d, want 1 after cancellation", got)
	}
}

func TestCollectCommand_HelpShowsImageFlag(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	output, err := runCollectArgs(context.Background(), t, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"--image",
		"Collect from a disk image file (DMG, E01, or raw); macfor attaches and detaches it read-only",
		"macfor collect --image ./suspect.dmg --output ./evidence.zip",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestValidateImagePath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "image.raw")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{name: "regular file", path: file},
		{name: "missing", path: filepath.Join(t.TempDir(), "missing.dmg"), wantErr: "image file does not exist"},
		{name: "directory", path: t.TempDir(), wantErr: "image path is a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateImagePath(tt.path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSourceHandle_NonCloserIsNoop(t *testing.T) {
	h := newSourceHandle(&mockSource{})
	if err := h.Close(); err != nil {
		t.Fatalf("Close on live-like source: %v", err)
	}
	onStart, beforeFinalize := h.custodyHooks()
	if onStart != nil || beforeFinalize != nil {
		t.Error("non-image source should have no custody hooks")
	}
	if got := h.describe(""); got != "" {
		t.Errorf("describe(live) = %q, want empty", got)
	}
}

func TestSourceHandle_CloseIdempotent(t *testing.T) {
	fake := &fakeImageSource{path: "/x.dmg"}
	h := newSourceHandle(fake)
	for range 3 {
		if err := h.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if got := fake.closes(); got != 1 {
		t.Errorf("underlying Close calls = %d, want 1", got)
	}
}
