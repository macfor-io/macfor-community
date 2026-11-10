package orchestrator

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/atime"
	"github.com/macfor-io/macfor-community/sources/directory"
)

// readZipJSON decodes the named JSON file from the container at path into v.
func readZipJSON(t *testing.T, path, name string, v any) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		return
	}
	t.Fatalf("%s not found in container", name)
}

// runCollection runs a full collection with cfg (Source and Container are
// filled in when unset) and returns the container path.
func runCollection(t *testing.T, cfg Config) (string, *Result) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	t.Cleanup(func() { _ = cont.Close() })
	if cfg.Source == nil {
		cfg.Source = &mockSource{}
	}
	cfg.Container = cont
	cfg.ToolVersion = "1.0.0"
	o, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path, result
}

func TestOrchestrator_RecordsConfiguredVolumes(t *testing.T) {
	defer restoreRegistry()()
	registerMockPlugins()

	vols := []atime.Volume{
		{MountPoint: "/", FSType: "apfs", Flags: []string{"read-only"}, Policy: atime.PolicyNone},
		{MountPoint: "/System/Volumes/Data", FSType: "apfs", Flags: []string{}, Policy: atime.PolicyLazy},
	}
	path, _ := runCollection(t, Config{Volumes: vols})

	var m container.Manifest
	readZipJSON(t, path, "manifest.json", &m)
	if len(m.Source.Volumes) != 2 || m.Source.Volumes[1].Policy != atime.PolicyLazy {
		t.Errorf("source.volumes = %+v, want the configured volumes", m.Source.Volumes)
	}
}

// readingPlugin reads path through the source during Detect and Collect.
type readingPlugin struct {
	id, path string
}

func (p *readingPlugin) Info() macfor.PluginInfo {
	return macfor.PluginInfo{ID: p.id, Name: p.id, Version: "1.0.0", Categories: []string{"test"}}
}

func (p *readingPlugin) Detect(_ context.Context, src macfor.Source) (macfor.DetectionResult, error) {
	if _, err := src.ReadDir(filepath.Dir(p.path)); err != nil {
		return macfor.DetectionResult{}, err
	}
	return macfor.DetectionResult{Applicable: true, Reason: "fixture present"}, nil
}

func (p *readingPlugin) Collect(_ context.Context, src macfor.Source, _ macfor.Collector) error {
	rc, err := src.Open(p.path)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	_, err = io.ReadAll(rc)
	return err
}

// stageOld writes content to path (when non-empty) and sets an access time
// well before its mtime, so the next read updates it even under lazy
// access-time updates.
func stageOld(t *testing.T, path, content string) {
	t.Helper()
	if content != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
}

func TestOrchestrator_RecordsAccessTimeChanges(t *testing.T) {
	root := t.TempDir()
	stageOld(t, filepath.Join(root, "Users/alice/.zsh_history"), ": 1:0;ls")
	stageOld(t, filepath.Join(root, "Users/alice"), "")

	vols := atime.ProbeSource(mustDirSource(t, root))
	if !atime.AnyMayUpdate(vols) {
		t.Skip("temp volume does not update access times")
	}

	defer useOnlyPlugin(&readingPlugin{id: "test.reader", path: "/Users/alice/.zsh_history"})()

	path, result := runCollection(t, Config{Source: mustDirSource(t, root)})

	if result.AccessTimes == nil {
		t.Fatal("Result.AccessTimes is nil, want a summary")
	}
	var m container.Manifest
	readZipJSON(t, path, "manifest.json", &m)
	if m.AccessTimes == nil {
		t.Fatal("manifest access_times missing")
	}
	if m.AccessTimes.Modified < 2 || len(m.AccessTimes.Inodes) < 2 || len(m.AccessTimes.Unobserved) == 0 {
		t.Errorf("access_times = %+v, want >=2 modified reads on 2 inodes", m.AccessTimes)
	}
	if len(m.Source.Volumes) == 0 {
		t.Error("source.volumes missing")
	}
	if m.AccessTimes.Unclosed != 0 {
		t.Errorf("unclosed = %d, want 0: a plugin read was never compared", m.AccessTimes.Unclosed)
	}

	var custody container.ChainOfCustodyLog
	readZipJSON(t, path, "chain-of-custody.json", &custody)
	phases := map[string]bool{}
	for _, e := range custody.Entries {
		if e.Action != container.CustodyActionAccessTimeModified {
			continue
		}
		if e.Details["plugin"] != "test.reader" {
			t.Errorf("event plugin = %v, want test.reader", e.Details["plugin"])
		}
		before, _ := e.Details["before"].(map[string]any)
		after, _ := e.Details["after"].(map[string]any)
		if before["atime"] == nil || after["atime"] == nil || before["atime"] == after["atime"] {
			t.Errorf("event %s lacks differing before/after atime: %v", e.Target, e.Details)
		}
		phases[e.Details["phase"].(string)+":"+e.Details["operation"].(string)+":"+e.Target] = true
	}
	for _, want := range []string{
		"detect:readdir:/Users/alice",
		"collect:read:/Users/alice/.zsh_history",
	} {
		if !phases[want] {
			t.Errorf("missing access_time_modified event %s; have %v", want, phases)
		}
	}
}

func TestOrchestrator_NoTrackingOnReadOnlyVolumes(t *testing.T) {
	root := t.TempDir()
	stageOld(t, filepath.Join(root, "Users/alice/.zsh_history"), "x")

	defer useOnlyPlugin(&readingPlugin{id: "test.reader", path: "/Users/alice/.zsh_history"})()

	// Volumes claim read-only, so no tracker is created and nothing observed.
	path, result := runCollection(t, Config{
		Source:  mustDirSource(t, root),
		Volumes: []atime.Volume{{MountPoint: root, Policy: atime.PolicyNone}},
	})
	if result.AccessTimes != nil {
		t.Errorf("AccessTimes = %+v, want nil when no volume may update access times", result.AccessTimes)
	}
	var m container.Manifest
	readZipJSON(t, path, "manifest.json", &m)
	if m.AccessTimes != nil {
		t.Errorf("manifest access_times = %+v, want absent", m.AccessTimes)
	}
}

func TestOrchestrator_SharedTrackerDeliversEarlierDetectionEvents(t *testing.T) {
	root := t.TempDir()
	stageOld(t, filepath.Join(root, "Users/alice/.zsh_history"), "x")
	stageOld(t, filepath.Join(root, "Users/alice"), "")
	if !atime.AnyMayUpdate(atime.ProbeSource(mustDirSource(t, root))) {
		t.Skip("temp volume does not update access times")
	}

	defer useOnlyPlugin(&readingPlugin{id: "test.reader", path: "/Users/alice/.zsh_history"})()

	tr := atime.NewTracker()
	src := mustDirSource(t, root)

	// A detection-only pass, as the CLI runs first, with no container.
	dry, err := New(Config{Source: src, DryRun: true, AccessTimes: tr})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dry.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	path, _ := runCollection(t, Config{Source: src, AccessTimes: tr})
	var custody container.ChainOfCustodyLog
	readZipJSON(t, path, "chain-of-custody.json", &custody)
	// The dry run's event was observed before the container's collection
	// started; the collection's own detection pass happens after.
	started := custody.CreatedAt
	var fromDryRun int
	for _, e := range custody.Entries {
		if e.Action == container.CustodyActionAccessTimeModified && e.Target == "/Users/alice" &&
			e.Details["phase"] == "detect" && e.Timestamp.Before(started) {
			fromDryRun++
		}
	}
	if fromDryRun != 1 {
		t.Errorf("dry-run detection events recorded = %d, want 1 (observed before %v)", fromDryRun, started)
	}
}

func mustDirSource(t *testing.T, root string) macfor.Source {
	t.Helper()
	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("directory.New: %v", err)
	}
	return src
}

// useOnlyPlugin replaces the plugin registry with p and returns a function
// that restores the previous registry, so these tests do not strip the real
// plugins from tests that run after them.
func useOnlyPlugin(p macfor.Plugin) func() {
	restore := restoreRegistry()
	macfor.ClearRegistry()
	macfor.Register(p)
	return restore
}

// restoreRegistry snapshots the plugin registry and returns a function that
// puts it back.
func restoreRegistry() func() {
	saved := macfor.All()
	return func() {
		macfor.ClearRegistry()
		for _, p := range saved {
			macfor.Register(p)
		}
	}
}

// closeAtEOF closes rc when a read reaches EOF, as a streaming reader handed
// to Collector.AddFile might. The container copies it while holding its own
// lock, so the observed Close runs under that lock.
type closeAtEOF struct{ rc io.ReadCloser }

func (c closeAtEOF) Read(p []byte) (int, error) {
	n, err := c.rc.Read(p)
	if err == io.EOF {
		_ = c.rc.Close()
	}
	return n, err
}

// copyingPlugin streams a source file into the container with closeAtEOF.
type copyingPlugin struct{ readingPlugin }

func (p *copyingPlugin) Collect(ctx context.Context, src macfor.Source, out macfor.Collector) error {
	rc, err := src.Open(p.path)
	if err != nil {
		return err
	}
	a, err := macfor.NewArtifact(p.id, "test", "history")
	if err != nil {
		return err
	}
	a.SourcePath = p.path
	return out.AddFile(ctx, *a, closeAtEOF{rc})
}

func TestOrchestrator_CloseInsideContainerCopyDoesNotDeadlock(t *testing.T) {
	root := t.TempDir()
	stageOld(t, filepath.Join(root, "Users/alice/.zsh_history"), "history")
	if !atime.AnyMayUpdate(atime.ProbeSource(mustDirSource(t, root))) {
		t.Skip("temp volume does not update access times")
	}
	defer useOnlyPlugin(&copyingPlugin{readingPlugin{id: "test.copier", path: "/Users/alice/.zsh_history"}})()

	done := make(chan *Result, 1)
	go func() {
		_, r := runCollection(t, Config{Source: mustDirSource(t, root)})
		done <- r
	}()
	select {
	case r := <-done:
		var streamed bool
		if r.AccessTimes != nil {
			for _, ic := range r.AccessTimes.Inodes {
				streamed = streamed || ic.Path == "/Users/alice/.zsh_history"
			}
		}
		if !streamed {
			t.Errorf("AccessTimes = %+v, want the streamed read recorded", r.AccessTimes)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("collection deadlocked closing an observed file inside the container's copy")
	}
}
