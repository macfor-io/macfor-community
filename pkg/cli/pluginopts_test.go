package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/rs/zerolog"
)

// optTestSecret is a sentinel secret value that must never be written out.
const optTestSecret = "s3cr3t-HUNTER2-value"

// optTestIDs are registered plugin IDs used by the parser tests. "ai" and
// "ai.claude" overlap "ai.claudedesktop" to exercise longest-prefix matching.
var optTestIDs = []string{"shell.history", "ai", "ai.claude", "ai.claudedesktop"}

// fakeOptEnv returns a pluginOptEnv backed by maps.
func fakeOptEnv(vars map[string]string, files map[string]string) pluginOptEnv {
	return pluginOptEnv{
		lookupEnv: func(k string) (string, bool) {
			v, ok := vars[k]
			return v, ok
		},
		readFile: func(p string) ([]byte, error) {
			v, ok := files[p]
			if !ok {
				return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
			}
			return []byte(v), nil
		},
	}
}

func TestParsePluginOpts_Valid(t *testing.T) {
	env := fakeOptEnv(
		map[string]string{"PW": optTestSecret, "EMPTY": ""},
		map[string]string{
			"/k/one":  optTestSecret + "\n",
			"/k/crlf": optTestSecret + "\r\n",
			"/k/two":  "line\n\n",
		},
	)

	tests := []struct {
		name      string
		entry     string
		plugin    string
		key       string
		value     string
		sensitive bool
	}{
		{"dot separator dotted id", "ai.claudedesktop.skip_content=true", "ai.claudedesktop", "skip_content", "true", false},
		{"colon separator dotted id", "ai.claudedesktop:skip_content=true", "ai.claudedesktop", "skip_content", "true", false},
		{"longest prefix wins over ai.claude", "ai.claudedesktop.x=1", "ai.claudedesktop", "x", "1", false},
		{"shorter id still matches", "ai.claude.x=1", "ai.claude", "x", "1", false},
		{"shortest id", "ai.x=1", "ai", "x", "1", false},
		{"key containing dots", "shell.history.a.b=1", "shell.history", "a.b", "1", false},
		{"value with equals and commas", "shell.history.q=a=b,c", "shell.history", "q", "a=b,c", false},
		{"empty literal", "shell.history.q=", "shell.history", "q", "", false},
		{"env source", "ai.claudedesktop.audit_key=env:PW", "ai.claudedesktop", "audit_key", optTestSecret, true},
		{"env source empty but set", "shell.history.q=env:EMPTY", "shell.history", "q", "", true},
		{"file source trims newline", "ai.claudedesktop.audit_key=file:/k/one", "ai.claudedesktop", "audit_key", optTestSecret, true},
		{"file source trims crlf", "shell.history.q=file:/k/crlf", "shell.history", "q", optTestSecret, true},
		{"file source trims only one newline", "shell.history.q=file:/k/two", "shell.history", "q", "line\n", true},
		{"literal escape", "shell.history.q=literal:env:PW", "shell.history", "q", "env:PW", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parsePluginOpts([]string{tt.entry}, optTestIDs, env)
			if err != nil {
				t.Fatalf("parsePluginOpts: %v", err)
			}
			got, ok := opts[tt.plugin][tt.key]
			if !ok {
				t.Fatalf("option %s.%s not parsed; got %v", tt.plugin, tt.key, opts.Redacted())
			}
			if got.Value != tt.value {
				t.Errorf("value = %q, want %q", got.Value, tt.value)
			}
			if got.Sensitive != tt.sensitive {
				t.Errorf("sensitive = %v, want %v", got.Sensitive, tt.sensitive)
			}
			if len(opts) != 1 || len(opts[tt.plugin]) != 1 {
				t.Errorf("expected exactly one option, got %v", opts.Redacted())
			}
		})
	}
}

func TestParsePluginOpts_MultiplePlugins(t *testing.T) {
	opts, err := parsePluginOpts([]string{
		"ai.claudedesktop.skip_content=true",
		"ai.claudedesktop:include_secrets=false",
		"shell.history.skip_fish=true",
	}, optTestIDs, fakeOptEnv(nil, nil))
	if err != nil {
		t.Fatalf("parsePluginOpts: %v", err)
	}
	if len(opts["ai.claudedesktop"]) != 2 || len(opts["shell.history"]) != 1 {
		t.Errorf("unexpected options: %v", opts.Redacted())
	}
}

func TestParsePluginOpts_NoEntries(t *testing.T) {
	opts, err := parsePluginOpts(nil, optTestIDs, fakeOptEnv(nil, nil))
	if err != nil || opts != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", opts, err)
	}
}

func TestParsePluginOpts_Errors(t *testing.T) {
	env := fakeOptEnv(map[string]string{"PW": optTestSecret}, nil)

	tests := []struct {
		name    string
		entries []string
		wantMsg string
	}{
		{"unknown plugin", []string{"ia.claudedesktop.audit_key=" + optTestSecret}, "unknown plugin"},
		{"plugin id prefix without separator", []string{"shell.historyx.k=1"}, "unknown plugin"},
		{"bare plugin id", []string{"shell.history=1"}, "missing option key"},
		{"missing equals", []string{"ai.claudedesktop.audit_key:" + optTestSecret}, "expected <plugin_id>.<key>=<value>"},
		{"missing key", []string{"ai.claudedesktop.=1"}, "missing option key"},
		{"whitespace in key", []string{"shell.history.a b=1"}, "whitespace"},
		{"missing env var", []string{"ai.claudedesktop.audit_key=env:NOPE_UNSET"}, "NOPE_UNSET is not set"},
		{"empty env name", []string{"shell.history.k=env:"}, "needs a variable name"},
		{"missing file", []string{"shell.history.k=file:/does/not/exist"}, "reading option value file"},
		{"empty file path", []string{"shell.history.k=file:"}, "needs a path"},
		{"duplicate key", []string{"shell.history.k=1", "shell.history:k=env:PW"}, "more than once"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePluginOpts(tt.entries, optTestIDs, env)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tt.wantMsg)
			}
			if strings.Contains(err.Error(), optTestSecret) {
				t.Errorf("error leaks the option value: %q", err)
			}
		})
	}
}

func TestParsePluginOpts_MalformedWraps(t *testing.T) {
	_, err := parsePluginOpts([]string{"shell.history.k"}, optTestIDs, fakeOptEnv(nil, nil))
	if !errors.Is(err, errMalformedPluginOpt) {
		t.Errorf("error %v does not wrap errMalformedPluginOpt", err)
	}
}

func TestParsePluginOpts_RealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(path, []byte(optTestSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := parsePluginOpts([]string{"shell.history.k=file:" + path}, optTestIDs, defaultPluginOptEnv)
	if err != nil {
		t.Fatalf("parsePluginOpts: %v", err)
	}
	if got := opts["shell.history"]["k"].Value; got != optTestSecret {
		t.Errorf("value = %q, want %q", got, optTestSecret)
	}
}

func TestWarnLiteralSecretOptions_NoValue(t *testing.T) {
	var buf bytes.Buffer
	saved := logger
	logger = zerolog.New(&buf)
	defer func() { logger = saved }()

	warnLiteralSecretOptions(container.PluginOptions{
		"ai.claudedesktop": {
			"audit_key":    {Value: optTestSecret},
			"skip_content": {Value: "true"},
		},
	})

	out := buf.String()
	if !strings.Contains(out, "audit_key") {
		t.Errorf("expected a warning naming audit_key, got %q", out)
	}
	if strings.Contains(out, "skip_content") {
		t.Errorf("unexpected warning for a non-secret key: %q", out)
	}
	if strings.Contains(out, optTestSecret) {
		t.Errorf("warning leaks the option value: %q", out)
	}
}

// registerOptionPlugin registers one applicable plugin whose Collect records
// the options it sees through macfor.OptionProvider.
func registerOptionPlugin(t *testing.T, id string, seen map[string]string, keys ...string) {
	t.Helper()
	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:         id,
			Name:       id,
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{Applicable: true, Reason: "always applicable"},
		collectCallback: func(_ context.Context, _ macfor.Source, out macfor.Collector) error {
			op, ok := out.(macfor.OptionProvider)
			if !ok {
				t.Errorf("collector for %s does not implement macfor.OptionProvider", id)
				return nil
			}
			for _, k := range keys {
				seen[id+"/"+k] = op.Option(k)
			}
			return nil
		},
	})
}

func TestCollectCommand_PluginOptsDryRunMasked(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()
	registerOptionPlugin(t, "ai.claudedesktop", map[string]string{})
	t.Setenv("MACFOR_TEST_PW", optTestSecret)

	for _, asJSON := range []bool{false, true} {
		resetCollectFlags()
		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)
		args := []string{
			"collect", "--dry-run", "--from", t.TempDir(),
			"--plugin-opt", "ai.claudedesktop.skip_content=true",
			"--plugin-opt", "ai.claudedesktop:audit_key=env:MACFOR_TEST_PW",
			"--plugin-opt", "ai.claudedesktop.token=" + optTestSecret,
		}
		if asJSON {
			args = append(args, "--json")
		}
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("json=%v: unexpected error: %v", asJSON, err)
		}

		out := buf.String()
		if strings.Contains(out, optTestSecret) {
			t.Fatalf("json=%v: dry-run output leaks the secret:\n%s", asJSON, out)
		}
		if !asJSON {
			for _, want := range []string{
				"Plugin options:",
				"ai.claudedesktop.skip_content = true",
				"ai.claudedesktop.audit_key = [set]",
				"ai.claudedesktop.token = [set]",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("dry-run output missing %q:\n%s", want, out)
				}
			}
			continue
		}

		var res CollectResult
		if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
			t.Fatalf("decoding JSON: %v\n%s", err, out)
		}
		want := map[string]string{"skip_content": "true", "audit_key": "[set]", "token": "[set]"}
		got := res.PluginOptions["ai.claudedesktop"]
		for k, v := range want {
			if got[k] != v {
				t.Errorf("plugin_options[%s] = %q, want %q", k, got[k], v)
			}
		}
	}
}

func TestCollectCommand_PluginOptsUnknownPluginFailsFast(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()
	seen := map[string]string{}
	registerOptionPlugin(t, "ai.claudedesktop", seen, "x")

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{
		"collect", "--from", t.TempDir(), "--output", outputPath,
		"--plugin-opt", "nope.plugin.x=1",
	})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown plugin") {
		t.Fatalf("expected unknown plugin error, got %v", err)
	}
	if len(seen) != 0 {
		t.Error("plugin ran despite invalid --plugin-opt")
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Error("evidence container created despite invalid --plugin-opt")
	}
}

func TestCollectCommand_PluginOptsReachPluginAndAreMasked(t *testing.T) {
	resetTestState()
	resetCollectFlags()
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	seen := map[string]string{}
	registerOptionPlugin(t, "ai.claudedesktop", seen, "skip_content", "audit_key", "passphrase_file")
	registerOptionPlugin(t, "ai.claude", seen, "skip_content", "audit_key")

	t.Setenv("MACFOR_TEST_PW", optTestSecret)
	keyFile := filepath.Join(t.TempDir(), "pass.txt")
	if err := os.WriteFile(keyFile, []byte(optTestSecret+"-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(t.TempDir(), "evidence.zip")
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{
		"collect", "--from", t.TempDir(), "--output", outputPath, "--json",
		"--plugin-opt", "ai.claudedesktop.skip_content=true",
		"--plugin-opt", "ai.claudedesktop.audit_key=env:MACFOR_TEST_PW",
		"--plugin-opt", "ai.claudedesktop:passphrase_file=file:" + keyFile,
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, buf.String())
	}

	// The plugin saw its own options, with real values.
	if seen["ai.claudedesktop/skip_content"] != "true" ||
		seen["ai.claudedesktop/audit_key"] != optTestSecret ||
		seen["ai.claudedesktop/passphrase_file"] != optTestSecret+"-file" {
		t.Errorf("ai.claudedesktop saw wrong options: %v", seen)
	}
	// A plugin whose ID is a prefix of another saw none of them.
	if seen["ai.claude/skip_content"] != "" || seen["ai.claude/audit_key"] != "" {
		t.Errorf("ai.claude saw options of another plugin: %v", seen)
	}

	if strings.Contains(buf.String(), optTestSecret) {
		t.Errorf("command output leaks the secret:\n%s", buf.String())
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(optTestSecret)) {
		t.Error("evidence container contains the secret value")
	}

	var res CollectResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("decoding JSON: %v", err)
	}
	got := res.PluginOptions["ai.claudedesktop"]
	if got["skip_content"] != "true" || got["audit_key"] != "[set]" || got["passphrase_file"] != "[set]" {
		t.Errorf("unexpected plugin_options in JSON: %v", res.PluginOptions)
	}
}
