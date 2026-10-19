package container

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// optSecret is a sentinel secret value that must never be written out.
const optSecret = "s3cr3t-HUNTER2-container"

func TestIsSecretOptionKey(t *testing.T) {
	secret := []string{"audit_key", "API_TOKEN", "client_secret", "Password", "passwd", "credentials", "auth", "oauth_header"}
	plain := []string{"skip_content", "include_raw", "vm_hash_limit_mb", "max_files"}
	for _, k := range secret {
		if !IsSecretOptionKey(k) {
			t.Errorf("IsSecretOptionKey(%q) = false, want true", k)
		}
	}
	for _, k := range plain {
		if IsSecretOptionKey(k) {
			t.Errorf("IsSecretOptionKey(%q) = true, want false", k)
		}
	}
}

func TestPluginOptions_Redacted(t *testing.T) {
	if got := PluginOptions(nil).Redacted(); got != nil {
		t.Errorf("Redacted() of nil = %v, want nil", got)
	}

	opts := PluginOptions{
		"ai.claudedesktop": {
			"skip_content": {Value: "true"},
			"audit_key":    {Value: optSecret},
			"from_env":     {Value: optSecret, Sensitive: true},
		},
	}
	got := opts.Redacted()["ai.claudedesktop"]
	want := map[string]string{"skip_content": "true", "audit_key": MaskedOptionValue, "from_env": MaskedOptionValue}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Redacted()[%s] = %q, want %q", k, got[k], v)
		}
	}
}

func TestCollector_OptionScopedToPlugin(t *testing.T) {
	c, err := NewContainer(Config{OutputPath: filepath.Join(t.TempDir(), "test.zip")})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	// Before options are set every lookup is empty.
	if got := NewCollector(c, "ai.claudedesktop").Option("skip_content"); got != "" {
		t.Errorf("Option() before SetPluginOptions = %q, want empty", got)
	}

	c.SetPluginOptions(PluginOptions{
		"ai.claudedesktop": {"skip_content": {Value: "true"}, "audit_key": {Value: optSecret, Sensitive: true}},
		"shell.history":    {"skip_fish": {Value: "yes"}},
	})

	desktop := NewCollector(c, "ai.claudedesktop")
	shell := NewCollector(c, "shell.history")
	other := NewCollector(c, "ai.claude")

	checks := []struct {
		coll *Collector
		key  string
		want string
	}{
		{desktop, "skip_content", "true"},
		{desktop, "audit_key", optSecret},
		{desktop, "skip_fish", ""},
		{desktop, "missing", ""},
		{shell, "skip_fish", "yes"},
		{shell, "skip_content", ""},
		{shell, "audit_key", ""},
		{other, "skip_content", ""},
		{other, "audit_key", ""},
	}
	for _, ck := range checks {
		if got := ck.coll.Option(ck.key); got != ck.want {
			t.Errorf("%s Option(%q) = %q, want %q", ck.coll.pluginID, ck.key, got, ck.want)
		}
	}

	// The anonymous interface plugins assert keeps working.
	var out macfor.Collector = desktop
	op, ok := out.(interface{ Option(name string) string })
	if !ok || op.Option("skip_content") != "true" {
		t.Error("collector does not satisfy the anonymous Option interface")
	}
	if got := macfor.CollectorOption(out, "skip_content"); got != "true" {
		t.Errorf("macfor.CollectorOption() = %q, want %q", got, "true")
	}
}

func TestSetPluginOptions_CopiesInput(t *testing.T) {
	c, err := NewContainer(Config{OutputPath: filepath.Join(t.TempDir(), "test.zip")})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	opts := PluginOptions{"p.x": {"k": {Value: "v"}}}
	c.SetPluginOptions(opts)
	opts["p.x"]["k"] = PluginOption{Value: "changed"}
	if got := NewCollector(c, "p.x").Option("k"); got != "v" {
		t.Errorf("Option() = %q after caller mutation, want %q", got, "v")
	}
}

func TestSetPluginOptions_ManifestAndCustodyMasked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.zip")
	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	c.SetCollectionMetadata(CollectionMeta{ToolVersion: "test", SourceType: "live"})
	c.SetPluginOptions(PluginOptions{
		"ai.claudedesktop": {
			"skip_content":  {Value: "true"},
			"audit_key":     {Value: optSecret},                  // Secret-looking key, literal.
			"pw_from_env":   {Value: optSecret, Sensitive: true}, // Env/file source.
			"neutral_named": {Value: optSecret, Sensitive: true}, // Non-secret key, env/file source.
		},
	})

	// A plugin that reads its options and logs only that it did.
	coll := NewCollector(c, "ai.claudedesktop")
	if coll.Option("audit_key") != optSecret {
		t.Fatal("collector did not receive the secret value")
	}
	coll.Log(macfor.LogInfo, "audit key configured: %v", coll.Option("audit_key") != "")
	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("Finalize() collector error = %v", err)
	}
	c.AddPluginSummary(summary)

	if err := c.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(optSecret)) {
		t.Fatal("evidence container contains the secret option value")
	}

	var m Manifest
	if err := json.Unmarshal([]byte(readFromZip(t, path, "manifest.json")), &m); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	got := m.PluginOptions["ai.claudedesktop"]
	want := map[string]string{
		"skip_content":  "true",
		"audit_key":     MaskedOptionValue,
		"pw_from_env":   MaskedOptionValue,
		"neutral_named": MaskedOptionValue,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("manifest plugin_options[%s] = %q, want %q", k, got[k], v)
		}
	}

	var custody ChainOfCustodyLog
	if err := json.Unmarshal([]byte(readFromZip(t, path, "chain-of-custody.json")), &custody); err != nil {
		t.Fatalf("decoding custody log: %v", err)
	}
	found := false
	for _, e := range custody.Entries {
		if e.Action != CustodyActionPluginOptions {
			continue
		}
		found = true
		options, _ := e.Details["options"].(map[string]any)
		desktop, _ := options["ai.claudedesktop"].(map[string]any)
		if desktop["skip_content"] != "true" || desktop["audit_key"] != MaskedOptionValue {
			t.Errorf("custody plugin_options details = %v", e.Details)
		}
	}
	if !found {
		t.Error("no plugin_options chain-of-custody entry")
	}
}

func TestSetPluginOptions_EmptyIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.zip")
	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	c.SetCollectionMetadata(CollectionMeta{ToolVersion: "test", SourceType: "live"})
	c.SetPluginOptions(nil)
	if err := c.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(readFromZip(t, path, "manifest.json")), []byte("plugin_options")) {
		t.Error("manifest has plugin_options although none were set")
	}
	if bytes.Contains([]byte(readFromZip(t, path, "chain-of-custody.json")), []byte(CustodyActionPluginOptions)) {
		t.Error("custody log has a plugin_options entry although none were set")
	}
}
