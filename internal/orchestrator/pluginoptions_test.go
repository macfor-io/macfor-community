package orchestrator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// orchSecret is a sentinel secret value that must never be written out.
const orchSecret = "s3cr3t-HUNTER2-orchestrator"

// optionSeeingPlugin is a fake plugin that records the options its collector
// exposes and writes a record proving it ran.
type optionSeeingPlugin struct {
	id   string
	keys []string
	seen map[string]string
}

func (p *optionSeeingPlugin) Info() macfor.PluginInfo {
	return macfor.PluginInfo{ID: p.id, Name: p.id, Version: "1.0.0", Categories: []string{"test"}}
}

func (p *optionSeeingPlugin) Detect(context.Context, macfor.Source) (macfor.DetectionResult, error) {
	return macfor.DetectionResult{Applicable: true, Reason: "always"}, nil
}

func (p *optionSeeingPlugin) Collect(ctx context.Context, _ macfor.Source, out macfor.Collector) error {
	for _, k := range p.keys {
		p.seen[k] = macfor.CollectorOption(out, k)
	}
	return out.AddRecord(ctx, macfor.Artifact{PluginID: p.id, Category: "test", Type: "seen"},
		map[string]bool{"audit_key_set": p.seen["audit_key"] != ""})
}

func TestRun_PluginOptionsReachPlugin(t *testing.T) {
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	desktop := &optionSeeingPlugin{id: "ai.claudedesktop", keys: []string{"skip_content", "audit_key"}, seen: map[string]string{}}
	prefix := &optionSeeingPlugin{id: "ai.claude", keys: []string{"skip_content", "audit_key"}, seen: map[string]string{}}
	macfor.Register(desktop)
	macfor.Register(prefix)

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer func() { _ = cont.Close() }()

	o, err := New(Config{
		Source:      &mockSource{},
		Container:   cont,
		ToolVersion: "test",
		PluginOptions: container.PluginOptions{
			"ai.claudedesktop": {
				"skip_content": {Value: "true"},
				"audit_key":    {Value: orchSecret, Sensitive: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if desktop.seen["skip_content"] != "true" || desktop.seen["audit_key"] != orchSecret {
		t.Errorf("ai.claudedesktop saw %v", desktop.seen)
	}
	if prefix.seen["skip_content"] != "" || prefix.seen["audit_key"] != "" {
		t.Errorf("ai.claude saw another plugin's options: %v", prefix.seen)
	}

	got := result.PluginOptions["ai.claudedesktop"]
	if got["skip_content"] != "true" || got["audit_key"] != container.MaskedOptionValue {
		t.Errorf("Result.PluginOptions = %v", result.PluginOptions)
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(orchSecret)) {
		t.Error("evidence container contains the secret option value")
	}
	if !bytes.Contains(raw, []byte(`"plugin_options"`)) {
		t.Error("evidence container does not record plugin_options")
	}
}

func TestRun_PluginOptionsDryRunResult(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source: &mockSource{},
		DryRun: true,
		PluginOptions: container.PluginOptions{
			"test.applicable": {"password": {Value: orchSecret}, "mode": {Value: "fast"}},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := result.PluginOptions["test.applicable"]
	if got["password"] != container.MaskedOptionValue || got["mode"] != "fast" {
		t.Errorf("Result.PluginOptions = %v", result.PluginOptions)
	}
}

func TestRun_PluginOptionsUnknownPlugin(t *testing.T) {
	cleanup := registerMockPlugins()
	defer cleanup()

	o, err := New(Config{
		Source:        &mockSource{},
		DryRun:        true,
		PluginOptions: container.PluginOptions{"no.such.plugin": {"k": {Value: orchSecret}}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = o.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no.such.plugin") {
		t.Fatalf("Run() error = %v, want unknown plugin error", err)
	}
	if strings.Contains(err.Error(), orchSecret) {
		t.Errorf("error leaks the option value: %v", err)
	}
}
