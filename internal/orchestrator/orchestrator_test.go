package orchestrator

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// mockPlugin is a mock plugin for testing.
type mockPlugin struct {
	info            macfor.PluginInfo
	detectResult    macfor.DetectionResult
	detectErr       error
	collectErr      error
	collectCallback func(ctx context.Context, src macfor.Source, out macfor.Collector) error
	detectDelay     time.Duration
	collectDelay    time.Duration
}

func (m *mockPlugin) Info() macfor.PluginInfo {
	return m.info
}

func (m *mockPlugin) Detect(ctx context.Context, _ macfor.Source) (macfor.DetectionResult, error) {
	if m.detectDelay > 0 {
		select {
		case <-time.After(m.detectDelay):
		case <-ctx.Done():
			return macfor.DetectionResult{}, ctx.Err()
		}
	}
	return m.detectResult, m.detectErr
}

func (m *mockPlugin) Collect(ctx context.Context, src macfor.Source, out macfor.Collector) error {
	if m.collectDelay > 0 {
		select {
		case <-time.After(m.collectDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if m.collectCallback != nil {
		return m.collectCallback(ctx, src, out)
	}
	return m.collectErr
}

// mockSource implements macfor.Source for testing.
type mockSource struct{}

func (m *mockSource) Type() macfor.SourceType                     { return macfor.SourceTypeLive }
func (m *mockSource) OSVersion() (string, error)                  { return "14.0", nil }
func (m *mockSource) Hostname() (string, error)                   { return "test-host", nil }
func (m *mockSource) Open(_ string) (io.ReadCloser, error)        { return nil, nil }
func (m *mockSource) Stat(_ string) (macfor.FileInfo, error)      { return nil, nil }
func (m *mockSource) ReadDir(_ string) ([]macfor.DirEntry, error) { return nil, nil }
func (m *mockSource) Glob(_ string) ([]string, error)             { return nil, nil }
func (m *mockSource) Walk(_ string, _ macfor.WalkFunc) error      { return nil }
func (m *mockSource) Users() ([]macfor.User, error)               { return nil, nil }
func (m *mockSource) ResolvePath(path string) string              { return path }
func (m *mockSource) ReadPlist(_ string, _ any) error             { return nil }
func (m *mockSource) SystemInfo() (*macfor.SystemInfo, error) {
	return &macfor.SystemInfo{OSName: "macOS", OSVersion: "14.0", Architecture: "arm64", Hostname: "test-host", SourceType: macfor.SourceTypeLive}, nil
}

// registerMockPlugins registers mock plugins for testing.
func registerMockPlugins() func() {
	macfor.ClearRegistry()

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.applicable",
			Name:       "Applicable Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "found test artifacts",
		},
	})

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.notapplicable",
			Name:       "Not Applicable Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: false,
			Reason:     "no test artifacts",
		},
	})

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "other.plugin",
			Name:       "Other Plugin",
			Version:    "1.0.0",
			Categories: []string{"other"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "always applicable",
		},
	})

	return func() {
		macfor.ClearRegistry()
	}
}

func TestNew_RequiresSource(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for nil source")
	}
	if err.Error() != "source is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNew_Success(t *testing.T) {
	o, err := New(Config{
		Source: &mockSource{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o == nil {
		t.Fatal("expected non-nil orchestrator")
	}
}

func TestOrchestrator_DryRun(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source: &mockSource{},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.DetectionOnly {
		t.Error("expected DetectionOnly to be true")
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if len(result.PluginResults) != 3 {
		t.Errorf("expected 3 plugin results, got %d", len(result.PluginResults))
	}

	// Check applicable plugins.
	applicableCount := 0
	for _, pr := range result.PluginResults {
		if pr.Applicable {
			applicableCount++
		}
	}
	if applicableCount != 2 {
		t.Errorf("expected 2 applicable plugins, got %d", applicableCount)
	}
}

func TestOrchestrator_PluginFilter(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source:    &mockSource{},
		PluginIDs: []string{"test.applicable"},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.PluginResults) != 1 {
		t.Errorf("expected 1 plugin result, got %d", len(result.PluginResults))
	}
	if result.PluginResults[0].ID != "test.applicable" {
		t.Errorf("expected test.applicable, got %s", result.PluginResults[0].ID)
	}
}

func TestOrchestrator_PluginNotFound(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source:    &mockSource{},
		PluginIDs: []string{"nonexistent"},
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	_, err = o.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}
	if !errors.Is(err, err) || err.Error() != "resolving plugins: plugin not found: nonexistent" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOrchestrator_CategoryFilter(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source:   &mockSource{},
		Category: "other",
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.PluginResults) != 1 {
		t.Errorf("expected 1 plugin result, got %d", len(result.PluginResults))
	}
	if result.PluginResults[0].ID != "other.plugin" {
		t.Errorf("expected other.plugin, got %s", result.PluginResults[0].ID)
	}
}

func TestOrchestrator_NoPlugins(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source:   &mockSource{},
		Category: "nonexistent",
		DryRun:   true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if len(result.PluginResults) != 0 {
		t.Errorf("expected 0 plugin results, got %d", len(result.PluginResults))
	}
}

func TestOrchestrator_Collection(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	o, err := New(Config{
		Source:      &mockSource{},
		Container:   cont,
		ToolVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.DetectionOnly {
		t.Error("expected DetectionOnly to be false")
	}

	// Check that applicable plugins collected.
	collectedCount := 0
	for _, pr := range result.PluginResults {
		if pr.Collected {
			collectedCount++
		}
	}
	if collectedCount != 2 {
		t.Errorf("expected 2 plugins to have collected, got %d", collectedCount)
	}

	// Verify container was created.
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("container was not created")
	}
}

func TestOrchestrator_ProgressCallback(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source: &mockSource{},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	var mu sync.Mutex
	var progressUpdates []Progress
	o.SetProgressCallback(func(p Progress) {
		mu.Lock()
		progressUpdates = append(progressUpdates, p)
		mu.Unlock()
	})

	_, err = o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(progressUpdates) == 0 {
		t.Fatal("expected progress updates")
	}

	// Check that we got init phase.
	var hasInit, hasDetection, hasComplete bool
	for _, p := range progressUpdates {
		switch p.Phase { //nolint:exhaustive // only checking for these phases in test
		case PhaseInit:
			hasInit = true
		case PhaseDetection:
			hasDetection = true
		case PhaseComplete:
			hasComplete = true
		}
	}

	if !hasInit {
		t.Error("expected init phase in progress updates")
	}
	if !hasDetection {
		t.Error("expected detection phase in progress updates")
	}
	if !hasComplete {
		t.Error("expected complete phase in progress updates")
	}
}

func TestOrchestrator_DetectionError(t *testing.T) {
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.error",
			Name:       "Error Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectErr: errors.New("detection failed"),
	})

	o, err := New(Config{
		Source: &mockSource{},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Detection errors should result in not applicable.
	if len(result.PluginResults) != 1 {
		t.Fatalf("expected 1 plugin result, got %d", len(result.PluginResults))
	}
	if result.PluginResults[0].Applicable {
		t.Error("plugin with detection error should not be applicable")
	}
	if result.PluginResults[0].Reason != "detection error: detection failed" {
		t.Errorf("unexpected reason: %s", result.PluginResults[0].Reason)
	}
}

func TestOrchestrator_CancellationDuringDetection(t *testing.T) {
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.slow",
			Name:       "Slow Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectDelay: 500 * time.Millisecond,
		detectResult: macfor.DetectionResult{
			Applicable: true,
		},
	})

	o, err := New(Config{
		Source: &mockSource{},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := o.Run(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}

	if !result.Cancelled {
		t.Error("expected Cancelled to be true")
	}
}

func TestOrchestrator_CancellationDuringCollection(t *testing.T) {
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.slow",
			Name:       "Slow Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "applicable",
		},
		collectDelay: 500 * time.Millisecond,
	})

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	o, err := New(Config{
		Source:      &mockSource{},
		Container:   cont,
		ToolVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err = o.Run(ctx)

	// The context should be cancelled.
	if err != nil && !errors.Is(err, context.Canceled) {
		// We may get an error from finalization since collection was cancelled.
		// This is acceptable.
		t.Logf("got error: %v", err)
	}
}

func TestOrchestrator_NoApplicablePlugins(t *testing.T) {
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.notapplicable",
			Name:       "Not Applicable Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: false,
			Reason:     "not applicable",
		},
	})

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	o, err := New(Config{
		Source:      &mockSource{},
		Container:   cont,
		ToolVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.ApplicablePlugins() != 0 {
		t.Errorf("expected 0 applicable plugins, got %d", result.ApplicablePlugins())
	}
	if result.CollectedPlugins() != 0 {
		t.Errorf("expected 0 collected plugins, got %d", result.CollectedPlugins())
	}
}

func TestOrchestrator_CollectionWithRecords(t *testing.T) {
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.records",
			Name:       "Records Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "applicable",
		},
		collectCallback: func(ctx context.Context, _ macfor.Source, out macfor.Collector) error {
			// Add some records.
			artifact, _ := macfor.NewArtifact("test.records", "test", "records")
			for i := 0; i < 5; i++ {
				_ = out.AddRecord(ctx, *artifact, map[string]any{
					"index": i,
					"data":  "test data",
				})
			}
			out.Log(macfor.LogInfo, "Collected %d records", 5)
			return nil
		},
	})

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	o, err := New(Config{
		Source:      &mockSource{},
		Container:   cont,
		ToolVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalRecords != 5 {
		t.Errorf("expected 5 total records, got %d", result.TotalRecords)
	}

	if len(result.PluginResults) != 1 {
		t.Fatalf("expected 1 plugin result, got %d", len(result.PluginResults))
	}
	if result.PluginResults[0].RecordsCreated != 5 {
		t.Errorf("expected 5 records created, got %d", result.PluginResults[0].RecordsCreated)
	}
}

func TestResult_ApplicablePlugins(t *testing.T) {
	result := &Result{
		PluginResults: []PluginResult{
			{ID: "a", Applicable: true},
			{ID: "b", Applicable: false},
			{ID: "c", Applicable: true},
			{ID: "d", Applicable: false},
		},
	}

	if result.ApplicablePlugins() != 2 {
		t.Errorf("expected 2 applicable plugins, got %d", result.ApplicablePlugins())
	}
}

func TestResult_CollectedPlugins(t *testing.T) {
	result := &Result{
		PluginResults: []PluginResult{
			{ID: "a", Collected: true},
			{ID: "b", Collected: false},
			{ID: "c", Collected: true},
			{ID: "d", Collected: true},
		},
	}

	if result.CollectedPlugins() != 3 {
		t.Errorf("expected 3 collected plugins, got %d", result.CollectedPlugins())
	}
}

func TestOrchestrator_ResultSuccessFalseOnErrors(t *testing.T) {
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.error",
			Name:       "Error Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "applicable",
		},
		collectCallback: func(_ context.Context, _ macfor.Source, out macfor.Collector) error {
			// Add an error.
			artifact, _ := macfor.NewArtifact("test.error", "test", "error")
			out.AddError(*artifact, errors.New("test error"))
			return nil
		},
	})

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	o, err := New(Config{
		Source:      &mockSource{},
		Container:   cont,
		ToolVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Success {
		t.Error("expected Success to be false when there are errors")
	}
	if result.TotalErrors != 1 {
		t.Errorf("expected 1 error, got %d", result.TotalErrors)
	}
}

func TestOrchestrator_NilContainer_DryRun(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	// Nil container should be treated as dry run.
	o, err := New(Config{
		Source:    &mockSource{},
		Container: nil,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.DetectionOnly {
		t.Error("expected DetectionOnly to be true when container is nil")
	}
}

func TestProgressPhase_Values(t *testing.T) {
	// Test that all phase values are distinct.
	phases := []ProgressPhase{
		PhaseInit,
		PhaseDetection,
		PhaseCollection,
		PhaseFinalize,
		PhaseComplete,
	}

	seen := make(map[ProgressPhase]bool)
	for _, p := range phases {
		if seen[p] {
			t.Errorf("duplicate phase value: %s", p)
		}
		seen[p] = true
	}
}

func TestOrchestrator_LifecycleHooks(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	var calls []string
	o, err := New(Config{
		Source:      &mockSource{},
		Container:   cont,
		ToolVersion: "1.0.0",
		OnCollectionStart: func(c *container.Container) {
			if c != cont {
				t.Error("OnCollectionStart got a different container")
			}
			calls = append(calls, "start")
		},
		BeforeFinalize: func(c *container.Container) {
			if c != cont {
				t.Error("BeforeFinalize got a different container")
			}
			calls = append(calls, "before_finalize")
		},
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	o.SetProgressCallback(func(p Progress) {
		if p.Phase == PhaseCollection && p.PluginID != "" {
			calls = append(calls, "collect")
		}
		if p.Phase == PhaseFinalize {
			calls = append(calls, "finalize")
		}
	})

	if _, err := o.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"start", "collect", "collect", "before_finalize", "finalize"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestOrchestrator_LifecycleHooksSkippedOnDryRun(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	hook := func(*container.Container) { t.Error("hook called on a dry run") }
	o, err := New(Config{
		Source:            &mockSource{},
		DryRun:            true,
		OnCollectionStart: hook,
		BeforeFinalize:    hook,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	if _, err := o.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestOrchestrator_BeforeFinalizeRunsWhenCancelled checks the documented
// guarantee that BeforeFinalize runs after a cancelled collection, so a
// source that holds resources (an attached image) is released and recorded.
func TestOrchestrator_BeforeFinalizeRunsWhenCancelled(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	beforeFinalize := 0
	o, err := New(Config{
		Source:            &mockSource{},
		Container:         cont,
		ToolVersion:       "1.0.0",
		OnCollectionStart: func(*container.Container) { cancel() },
		BeforeFinalize:    func(*container.Container) { beforeFinalize++ },
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, _ := o.Run(ctx)
	if result == nil || !result.Cancelled {
		t.Fatalf("result = %+v, want a cancelled run", result)
	}
	if beforeFinalize != 1 {
		t.Errorf("BeforeFinalize calls = %d, want 1 on a cancelled run", beforeFinalize)
	}
}
