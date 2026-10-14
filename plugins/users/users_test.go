package users

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/directory"
	"github.com/macfor-io/macfor-community/testdata/macostree"
)

// Secret marker values written into fixture plists. None may appear in any
// record the plugin produces.
const (
	secretShadowHash = "SECRET-SHADOW-HASH-BYTES"
	secretKerberos   = "SECRET-KERBEROS-KEY"
	secretSRP        = "SECRET-SRP-KEY"
	secretHint       = "SECRET-PASSWORD-HINT"
)

// dslocalUsersDir is the account plist directory relative to a source root.
var dslocalUsersDir = filepath.Join("private", "var", "db", "dslocal", "nodes", "Default", "users")

// recordingCollector captures records, files, and errors from a plugin.
type recordingCollector struct {
	files     int
	records   []any
	artifacts []macfor.Artifact
	errors    []error
}

func (c *recordingCollector) AddFile(_ context.Context, _ macfor.Artifact, src io.Reader) error {
	_, err := io.Copy(io.Discard, src)
	c.files++
	return err
}

func (c *recordingCollector) AddRecord(_ context.Context, a macfor.Artifact, record any) error {
	c.records = append(c.records, record)
	c.artifacts = append(c.artifacts, a)
	return nil
}

func (c *recordingCollector) AddError(_ macfor.Artifact, err error) {
	c.errors = append(c.errors, err)
}

//nolint:goprintffuncname // Implements macfor.Collector interface which uses Log not Logf.
func (c *recordingCollector) Log(macfor.LogLevel, string, ...any) {}

// fullAccountPlist returns a binary account plist with metadata, an embedded
// accountPolicyData plist, and every kind of credential material.
func fullAccountPlist(t *testing.T) []byte {
	t.Helper()
	policy, err := plist.Marshal(map[string]any{
		"creationTime":         1700000000.5,
		"passwordLastSetTime":  1700100000.0,
		"failedLoginCount":     3,
		"failedLoginTimestamp": 1700200000.0,
	}, plist.XMLFormat)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	data, err := plist.Marshal(map[string]any{
		"name":         []string{"alice", "alice@example.com"},
		"realname":     []string{"Alice Smith"},
		"uid":          []string{"501"},
		"gid":          []string{"20"},
		"generateduid": []string{"4A1B2C3D-0000-1111-2222-333344445555"},
		"home":         []string{"/Users/alice"},
		"shell":        []string{"/bin/zsh"},
		"IsHidden":     []string{"1"},
		"authentication_authority": []string{
			";ShadowHash;HASHLIST:<SALTED-SHA512-PBKDF2,SRP-RFC5054-4096-SHA512-PBKDF2>",
			";Kerberosv5;;alice@LKDC:SHA1.ABC;LKDC:SHA1.ABC;",
			";SecureToken;",
		},
		"accountPolicyData": [][]byte{policy},
		"ShadowHashData":    [][]byte{[]byte(secretShadowHash)},
		"KerberosKeys":      [][]byte{[]byte(secretKerberos)},
		"HeimdalSRPKey":     [][]byte{[]byte(secretSRP)},
		"hint":              []string{secretHint},
		"passwd":            []string{"********"},
	}, plist.BinaryFormat)
	if err != nil {
		t.Fatalf("marshal account: %v", err)
	}
	return data
}

func TestParseAccountFull(t *testing.T) {
	rec, err := parseAccount(fullAccountPlist(t))
	if err != nil {
		t.Fatalf("parseAccount() error = %v", err)
	}

	if rec.Type != typeUserAccount {
		t.Errorf("Type = %q, want %q", rec.Type, typeUserAccount)
	}
	if rec.Username != "alice" {
		t.Errorf("Username = %q, want alice", rec.Username)
	}
	if len(rec.Aliases) != 1 || rec.Aliases[0] != "alice@example.com" {
		t.Errorf("Aliases = %v, want [alice@example.com]", rec.Aliases)
	}
	if rec.RealName != "Alice Smith" || rec.HomeDir != "/Users/alice" || rec.Shell != "/bin/zsh" {
		t.Errorf("unexpected identity fields: %+v", rec)
	}
	if rec.UID == nil || *rec.UID != 501 || rec.GID == nil || *rec.GID != 20 {
		t.Errorf("UID/GID = %v/%v, want 501/20", rec.UID, rec.GID)
	}
	if rec.GeneratedUID != "4A1B2C3D-0000-1111-2222-333344445555" {
		t.Errorf("GeneratedUID = %q", rec.GeneratedUID)
	}
	if !rec.Hidden || rec.ServiceAccount || !rec.HasPassword || !rec.SecureToken {
		t.Errorf("flags hidden=%v service=%v password=%v securetoken=%v, want true/false/true/true",
			rec.Hidden, rec.ServiceAccount, rec.HasPassword, rec.SecureToken)
	}

	wantCreated := time.Unix(1700000000, 500000000).UTC()
	if rec.CreationTime == nil || !rec.CreationTime.Equal(wantCreated) {
		t.Errorf("CreationTime = %v, want %v", rec.CreationTime, wantCreated)
	}
	if rec.PasswordLastSetTime == nil || rec.PasswordLastSetTime.Unix() != 1700100000 {
		t.Errorf("PasswordLastSetTime = %v", rec.PasswordLastSetTime)
	}
	if rec.FailedLoginTime == nil || rec.FailedLoginTime.Unix() != 1700200000 {
		t.Errorf("FailedLoginTime = %v", rec.FailedLoginTime)
	}
	if rec.FailedLoginCount == nil || *rec.FailedLoginCount != 3 {
		t.Errorf("FailedLoginCount = %v, want 3", rec.FailedLoginCount)
	}
}

// TestParseAccountExcludesCredentials guards the metadata-only contract: no
// password hash, key, or hint may reach a record.
func TestParseAccountExcludesCredentials(t *testing.T) {
	rec, err := parseAccount(fullAccountPlist(t))
	if err != nil {
		t.Fatalf("parseAccount() error = %v", err)
	}
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	for _, secret := range []string{secretShadowHash, secretKerberos, secretSRP, secretHint, "********"} {
		if strings.Contains(string(out), secret) {
			t.Errorf("record contains credential material %q: %s", secret, out)
		}
	}
}

func TestParseAccountServiceAccount(t *testing.T) {
	tests := []struct {
		name  string
		uid   string
		shell string
		want  bool
	}{
		{"daemon uid", "1", "/usr/bin/false", true},
		{"low uid with shell", "250", "/bin/zsh", true},
		{"human uid nologin", "502", "/usr/bin/false", true},
		{"human", "502", "/bin/zsh", false},
		{"missing uid", "", "/bin/zsh", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct := map[string]any{"name": []string{"x"}, "shell": []string{tt.shell}}
			if tt.uid != "" {
				acct["uid"] = []string{tt.uid}
			}
			data, err := plist.Marshal(acct, plist.BinaryFormat)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := parseAccount(data)
			if err != nil {
				t.Fatalf("parseAccount() error = %v", err)
			}
			if rec.ServiceAccount != tt.want {
				t.Errorf("ServiceAccount = %v, want %v", rec.ServiceAccount, tt.want)
			}
		})
	}
}

func TestParseAccountErrors(t *testing.T) {
	noName, err := plist.Marshal(map[string]any{"uid": []string{"501"}}, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"not a plist": []byte("not a plist"),
		"no name":     noName,
	} {
		if _, err := parseAccount(data); err == nil {
			t.Errorf("%s: parseAccount() error = nil, want error", name)
		}
	}
}

func TestParseAccountMalformedPolicy(t *testing.T) {
	data, err := plist.Marshal(map[string]any{
		"name":              []string{"bob"},
		"uid":               []string{"502"},
		"accountPolicyData": [][]byte{[]byte("garbage")},
	}, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := parseAccount(data)
	if err != nil {
		t.Fatalf("parseAccount() error = %v", err)
	}
	if rec.CreationTime != nil || rec.FailedLoginCount != nil {
		t.Errorf("malformed policy should leave times unset, got %+v", rec)
	}
}

// fixtureSource builds a synthetic macOS tree with two users and replaces
// alice's account plist with a full one, including credential material.
func fixtureSource(t *testing.T) (macfor.Source, string) {
	t.Helper()
	opts := macostree.Defaults()
	opts.Users = append(opts.Users, macostree.UserOptions{
		Username: "_svc", UID: 250, GID: 250, Name: "Service", Shell: "/usr/bin/false",
	})
	root := macostree.MustBuild(t, opts)
	if err := os.WriteFile(filepath.Join(root, dslocalUsersDir, "alice.plist"), fullAccountPlist(t), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("directory.New() error = %v", err)
	}
	return src, root
}

func TestDetect(t *testing.T) {
	src, _ := fixtureSource(t)
	result, err := (&Plugin{}).Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !result.Applicable {
		t.Fatalf("Applicable = false, reason %q", result.Reason)
	}
	if got := result.Metadata["account_plists"]; got != 2 {
		t.Errorf("account_plists = %v, want 2", got)
	}
}

func TestDetectNoAccounts(t *testing.T) {
	root := t.TempDir()
	opts := macostree.Defaults()
	opts.Users = nil
	if err := macostree.Build(root, opts); err != nil {
		t.Fatal(err)
	}
	src, err := directory.New(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Plugin{}).Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Applicable {
		t.Error("Applicable = true for a source with no account plists")
	}
}

func TestCollect(t *testing.T) {
	src, root := fixtureSource(t)
	out := &recordingCollector{}
	if err := (&Plugin{}).Collect(context.Background(), src, out); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if out.files != 0 {
		t.Errorf("collected %d raw files, want 0 (metadata only)", out.files)
	}
	if len(out.errors) != 0 {
		t.Errorf("unexpected errors: %v", out.errors)
	}
	if len(out.records) != 2 {
		t.Fatalf("got %d records, want 2", len(out.records))
	}

	byName := make(map[string]AccountRecord)
	for i, r := range out.records {
		rec, ok := r.(AccountRecord)
		if !ok {
			t.Fatalf("record %d is %T, want AccountRecord", i, r)
		}
		byName[rec.Username] = rec
		if out.artifacts[i].Username != rec.Username {
			t.Errorf("artifact username = %q, want %q", out.artifacts[i].Username, rec.Username)
		}
		if out.artifacts[i].Type != artifactKindAccounts {
			t.Errorf("artifact type = %q, want %q", out.artifacts[i].Type, artifactKindAccounts)
		}
	}

	alice, ok := byName["alice"]
	if !ok {
		t.Fatal("no record for alice")
	}
	raw, err := os.ReadFile(filepath.Join(root, dslocalUsersDir, "alice.plist"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if alice.SourceSHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SourceSHA256 = %q, want hash of the source plist", alice.SourceSHA256)
	}
	if want := "/private/var/db/dslocal/nodes/Default/users/alice.plist"; alice.SourceFile != want {
		t.Errorf("SourceFile = %q, want %q", alice.SourceFile, want)
	}

	svc, ok := byName["_svc"]
	if !ok {
		t.Fatal("no record for _svc: service accounts must be recorded too")
	}
	if !svc.ServiceAccount {
		t.Error("_svc ServiceAccount = false, want true")
	}

	all := fmt.Sprint(out.records)
	for _, secret := range []string{secretShadowHash, secretKerberos, secretSRP, secretHint} {
		if strings.Contains(all, secret) {
			t.Errorf("collected records contain credential material %q", secret)
		}
	}
}

func TestCollectReportsUnparsablePlist(t *testing.T) {
	src, root := fixtureSource(t)
	if err := os.WriteFile(filepath.Join(root, dslocalUsersDir, "broken.plist"), []byte("not a plist"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := &recordingCollector{}
	if err := (&Plugin{}).Collect(context.Background(), src, out); err == nil {
		t.Error("Collect() error = nil, want error for an unparsable plist")
	}
	if len(out.records) != 2 {
		t.Errorf("got %d records, want 2: one bad plist must not stop the others", len(out.records))
	}
	if len(out.errors) != 1 {
		t.Errorf("got %d recorded errors, want 1", len(out.errors))
	}
}

func TestPluginRegistered(t *testing.T) {
	p, ok := macfor.ByID(PluginID)
	if !ok {
		t.Fatal("system.users plugin not registered")
	}
	if p.Info().ID != PluginID {
		t.Errorf("registered plugin ID = %q", p.Info().ID)
	}
}
