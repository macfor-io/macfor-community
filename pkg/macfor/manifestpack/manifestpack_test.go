package manifestpack

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// cleanManifest is a minimal, fully declarative manifest (no parser, no raw SQL)
// that passes both the artifacts validator and the pack constraints.
const cleanManifest = `schema_version: "1.0"
plugin:
  id: browser.safari
  name: Safari
  description: Safari browser artifacts.
  version: 1.0.0
  categories: [history]
  platforms:
    - os: darwin
artifacts:
  - id: history
    name: History
    category: history
    format: sqlite
    paths:
      - pattern: "~/Library/Safari/History.db"
    extract:
      from: history_items
      fields:
        - name: url
          column: history_items.url
`

// secondCleanManifest is a second declarative manifest for a different plugin.
const secondCleanManifest = `schema_version: "1.0"
plugin:
  id: shell.history
  name: Shell History
  description: Shell command history.
  version: 1.0.0
  categories: [command_history]
  platforms:
    - os: darwin
artifacts:
  - id: zsh
    name: Zsh History
    category: command_history
    format: text
    paths:
      - pattern: "~/.zsh_history"
`

// parserManifest references a native parser — forbidden in a pack.
const parserManifest = `schema_version: "1.0"
plugin:
  id: browser.safari
  name: Safari
  description: Safari browser artifacts.
  version: 1.0.0
  categories: [bookmarks]
  platforms:
    - os: darwin
artifacts:
  - id: bookmarks
    name: Bookmarks
    category: bookmarks
    format: binary_plist
    parser: safari.bookmarks
    paths:
      - pattern: "~/Library/Safari/Bookmarks.plist"
`

// whereManifest carries a raw WHERE clause — forbidden in a pack.
const whereManifest = `schema_version: "1.0"
plugin:
  id: browser.safari
  name: Safari
  description: Safari browser artifacts.
  version: 1.0.0
  categories: [history]
  platforms:
    - os: darwin
artifacts:
  - id: history
    name: History
    category: history
    format: sqlite
    paths:
      - pattern: "~/Library/Safari/History.db"
    extract:
      from: history_items
      where: "visit_count > 0"
      fields:
        - name: url
          column: history_items.url
`

// derivedManifest references a field-level derive function — native-code
// selection, forbidden in a pack.
const derivedManifest = `schema_version: "1.0"
plugin:
  id: browser.safari
  name: Safari
  description: Safari browser artifacts.
  version: 1.0.0
  categories: [history]
  platforms:
    - os: darwin
artifacts:
  - id: history
    name: History
    category: history
    format: sqlite
    paths:
      - pattern: "~/Library/Safari/History.db"
    extract:
      from: history_items
      fields:
        - name: url
          column: url
        - name: kind
          derived: safari.something
`

// badFromManifest has a non-identifier FROM target — forbidden in a pack.
const badFromManifest = `schema_version: "1.0"
plugin:
  id: browser.safari
  name: Safari
  description: Safari browser artifacts.
  version: 1.0.0
  categories: [history]
  platforms:
    - os: darwin
artifacts:
  - id: history
    name: History
    category: history
    format: sqlite
    paths:
      - pattern: "~/Library/Safari/History.db"
    extract:
      from: "t; DROP TABLE history"
      fields:
        - name: url
          column: url
`

// buildPackFS returns an fstest.MapFS holding a pack at the root composed of the
// given pluginID -> manifest-bytes map, with a correct pack.yaml. The signature
// is not written; tests add it as needed.
func buildPackFS(t *testing.T, version string, manifests map[string]string) fstest.MapFS {
	t.Helper()

	fsys := fstest.MapFS{}
	var entriesYAML strings.Builder
	entriesYAML.WriteString("pack_version: \"" + version + "\"\n")
	entriesYAML.WriteString("created_at: \"2026-06-20T00:00:00Z\"\n")
	entriesYAML.WriteString("entries:\n")

	// Deterministic ordering is irrelevant for correctness but keeps tests stable.
	for pluginID, body := range manifests {
		file := "manifests/" + pluginID + ".yaml"
		fsys[file] = &fstest.MapFile{Data: []byte(body)}
		sum := sha256.Sum256([]byte(body))
		entriesYAML.WriteString("  - plugin_id: " + pluginID + "\n")
		entriesYAML.WriteString("    file: " + file + "\n")
		entriesYAML.WriteString("    sha256: \"" + hex.EncodeToString(sum[:]) + "\"\n")
	}

	fsys[PackFile] = &fstest.MapFile{Data: []byte(entriesYAML.String())}
	return fsys
}

// genKey returns a fresh ed25519 keypair for a test.
func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return pub, priv
}

func TestLoadCleanPack(t *testing.T) {
	fsys := buildPackFS(t, "1.0", map[string]string{
		"browser.safari": cleanManifest,
		"shell.history":  secondCleanManifest,
	})

	pack, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if pack.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", pack.Version)
	}
	if len(pack.Manifests) != 2 {
		t.Fatalf("got %d manifests, want 2", len(pack.Manifests))
	}
	if pack.Manifests["browser.safari"] == nil {
		t.Error("missing browser.safari manifest")
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	fsys := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	pack, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	pub, priv := genKey(t)
	sig := Sign(pack, priv)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	if err := Verify(pack, sigB64, pub); err != nil {
		t.Errorf("Verify() with matching key error = %v, want nil", err)
	}
}

func TestVerifyTamperRejected(t *testing.T) {
	// Build, sign, then mutate the manifest bytes AND its declared hash so Load
	// succeeds, but the signature (bound to the original digest) no longer matches.
	fsys := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	pack, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	pub, priv := genKey(t)
	sigB64 := base64.StdEncoding.EncodeToString(Sign(pack, priv))

	// Tampered pack: change the manifest content and rebuild pack.yaml so the
	// on-disk hashes are internally consistent (Load passes) but the digest
	// differs from the signed one.
	tampered := strings.Replace(cleanManifest, "history_items.url", "history_items.title", 1)
	tamperedFS := buildPackFS(t, "1.0", map[string]string{"browser.safari": tampered})
	tamperedPack, err := Load(tamperedFS, ".")
	if err != nil {
		t.Fatalf("Load(tampered) error = %v", err)
	}

	if err := Verify(tamperedPack, sigB64, pub); err == nil {
		t.Error("Verify() accepted a tampered pack; want rejection")
	}
}

func TestLoadHashMismatchRejected(t *testing.T) {
	// On-disk tamper: the manifest bytes do not match the declared sha256 in
	// pack.yaml. Load must reject before parsing.
	fsys := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	// Corrupt the manifest file bytes without updating pack.yaml's hash.
	fsys["manifests/browser.safari.yaml"] = &fstest.MapFile{
		Data: []byte(strings.Replace(cleanManifest, "Safari", "Tampered", 1)),
	}

	_, err := Load(fsys, ".")
	if err == nil {
		t.Fatal("Load() accepted a hash-mismatched manifest; want rejection")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("error = %v, want hash mismatch", err)
	}
}

func TestVerifyForgeryRejected(t *testing.T) {
	fsys := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	pack, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	_, priv := genKey(t)
	sigB64 := base64.StdEncoding.EncodeToString(Sign(pack, priv))

	// A different (wrong) public key must fail verification.
	wrongPub, _ := genKey(t)
	if err := Verify(pack, sigB64, wrongPub); err == nil {
		t.Error("Verify() accepted a signature under the wrong key; want rejection")
	}
}

func TestVerifyGarbageSignature(t *testing.T) {
	fsys := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	pack, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	pub, _ := genKey(t)

	cases := map[string]string{
		"empty":        "",
		"not base64":   "!!!! not base64 !!!!",
		"wrong length": base64.StdEncoding.EncodeToString([]byte("too short")),
		"valid base64 garbage": base64.StdEncoding.EncodeToString(
			make([]byte, ed25519.SignatureSize)),
	}
	for name, sig := range cases {
		t.Run(name, func(t *testing.T) {
			// Must return an error, never panic.
			if err := Verify(pack, sig, pub); err == nil {
				t.Errorf("Verify(%q) = nil, want error", name)
			}
		})
	}
}

func TestVerifyWrongKeySize(t *testing.T) {
	fsys := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	pack, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := Verify(pack, "AAAA", ed25519.PublicKey([]byte{1, 2, 3})); err == nil {
		t.Error("Verify() accepted an undersized public key; want error")
	}
}

func TestConstraintEnforcement(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantErr  bool
		errFrag  string
	}{
		{"clean declarative", cleanManifest, false, ""},
		{"native parser", parserManifest, true, "native parser"},
		{"derive function", derivedManifest, true, "derive"},
		{"raw where clause", whereManifest, true, "extract.where"},
		{"non-identifier from", badFromManifest, true, "extract.from"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := buildPackFS(t, "1.0", map[string]string{"browser.safari": tt.manifest})
			_, err := Load(fsys, ".")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() = nil, want error")
				}
				if tt.errFrag != "" && !strings.Contains(err.Error(), tt.errFrag) {
					t.Errorf("error = %v, want fragment %q", err, tt.errFrag)
				}
				return
			}
			if err != nil {
				t.Errorf("Load() error = %v, want nil", err)
			}
		})
	}
}

func TestProvenanceStableAcrossReloads(t *testing.T) {
	fsys := buildPackFS(t, "1.0", map[string]string{
		"browser.safari": cleanManifest,
		"shell.history":  secondCleanManifest,
	})

	p1, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	p2, err := Load(fsys, ".")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	d1 := hex.EncodeToString(Digest(p1))
	d2 := hex.EncodeToString(Digest(p2))
	if d1 != d2 {
		t.Errorf("digest not stable across reloads: %s != %s", d1, d2)
	}

	prov := BuildProvenance(p1)
	if prov.Digest != d1 {
		t.Errorf("Provenance digest %s != Digest %s", prov.Digest, d1)
	}
	if prov.PackVersion != "1.0" {
		t.Errorf("Provenance version = %q, want 1.0", prov.PackVersion)
	}
	if len(prov.Entries) != 2 {
		t.Fatalf("got %d provenance entries, want 2", len(prov.Entries))
	}
	// Entries must be sorted by plugin ID for reproducibility.
	if prov.Entries[0].PluginID != "browser.safari" || prov.Entries[1].PluginID != "shell.history" {
		t.Errorf("provenance entries not sorted by plugin ID: %+v", prov.Entries)
	}
}

func TestDigestChangesWhenManifestChanges(t *testing.T) {
	fsysA := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	fsysB := buildPackFS(t, "1.0", map[string]string{
		"browser.safari": strings.Replace(cleanManifest, "history_items.url", "history_items.title", 1),
	})

	packA, err := Load(fsysA, ".")
	if err != nil {
		t.Fatalf("Load(A) error = %v", err)
	}
	packB, err := Load(fsysB, ".")
	if err != nil {
		t.Fatalf("Load(B) error = %v", err)
	}

	if hex.EncodeToString(Digest(packA)) == hex.EncodeToString(Digest(packB)) {
		t.Error("digest did not change when manifest content changed")
	}
}

func TestDigestChangesWhenVersionChanges(t *testing.T) {
	fsysA := buildPackFS(t, "1.0", map[string]string{"browser.safari": cleanManifest})
	fsysB := buildPackFS(t, "2.0", map[string]string{"browser.safari": cleanManifest})

	packA, err := Load(fsysA, ".")
	if err != nil {
		t.Fatalf("Load(A) error = %v", err)
	}
	packB, err := Load(fsysB, ".")
	if err != nil {
		t.Fatalf("Load(B) error = %v", err)
	}

	if hex.EncodeToString(Digest(packA)) == hex.EncodeToString(Digest(packB)) {
		t.Error("digest did not change when pack version changed")
	}
}

// TestFromManifests builds a provenance pack from in-memory trusted manifests
// (the embedded-collection use case): it must NOT enforce pack constraints
// (parsers/derivers/raw SQL are allowed for the binary's own manifests), the
// digest must be deterministic, and changing the version or a manifest must
// change the digest.
func TestFromManifests(t *testing.T) {
	// A trusted manifest using a native parser — Load would reject this, but
	// FromManifests must accept it.
	withParser, err := artifacts.Parse([]byte(parserManifest))
	if err != nil {
		t.Fatalf("parse parserManifest: %v", err)
	}
	clean, err := artifacts.Parse([]byte(cleanManifest))
	if err != nil {
		t.Fatalf("parse cleanManifest: %v", err)
	}

	mans := map[string]*artifacts.Manifest{"browser.safari": withParser, "system.x": clean}

	p1, err := FromManifests("tool-1.2.3", mans)
	if err != nil {
		t.Fatalf("FromManifests: %v", err)
	}
	if len(p1.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(p1.Entries))
	}

	// Determinism: same inputs → same digest across calls.
	p2, _ := FromManifests("tool-1.2.3", mans)
	if hex.EncodeToString(Digest(p1)) != hex.EncodeToString(Digest(p2)) {
		t.Error("digest not deterministic across identical FromManifests calls")
	}

	// Version binds the digest.
	p3, _ := FromManifests("tool-9.9.9", mans)
	if hex.EncodeToString(Digest(p1)) == hex.EncodeToString(Digest(p3)) {
		t.Error("digest did not change when the version changed")
	}

	// A changed manifest changes the digest.
	mans2 := map[string]*artifacts.Manifest{"browser.safari": withParser, "system.x": withParser}
	p4, _ := FromManifests("tool-1.2.3", mans2)
	if hex.EncodeToString(Digest(p1)) == hex.EncodeToString(Digest(p4)) {
		t.Error("digest did not change when a manifest's content changed")
	}

	prov := BuildProvenance(p1)
	if prov.PackVersion != "tool-1.2.3" || prov.Digest == "" || len(prov.Entries) != 2 {
		t.Errorf("unexpected provenance: %+v", prov)
	}
}

// TestFromManifestsHashesSourceBytes pins the provenance hash to the exact
// manifest file bytes, so it can be checked with `shasum -a 256 artifacts.yaml`
// and matches the hash a pack declares for the same file.
func TestFromManifestsHashesSourceBytes(t *testing.T) {
	src := []byte(cleanManifest)
	m, err := artifacts.Parse(src)
	if err != nil {
		t.Fatalf("parse cleanManifest: %v", err)
	}

	p, err := FromManifests("tool-1.2.3", map[string]*artifacts.Manifest{"browser.safari": m})
	if err != nil {
		t.Fatalf("FromManifests: %v", err)
	}
	sum := sha256.Sum256(src)
	if got, want := p.Entries[0].SHA256, hex.EncodeToString(sum[:]); got != want {
		t.Errorf("entry sha256 = %s, want sha256 of the source bytes %s", got, want)
	}

	// A comment-only edit doesn't change the parsed manifest, but it does
	// change the file, so it must change the hash.
	commented, err := artifacts.Parse(append([]byte("# edited\n"), src...))
	if err != nil {
		t.Fatalf("parse commented manifest: %v", err)
	}
	p2, _ := FromManifests("tool-1.2.3", map[string]*artifacts.Manifest{"browser.safari": commented})
	if p2.Entries[0].SHA256 == p.Entries[0].SHA256 {
		t.Error("hash did not change when the manifest file changed")
	}

	// A manifest built in code has no source bytes to attest to.
	if _, err := FromManifests("tool-1.2.3", map[string]*artifacts.Manifest{"x": {}}); err == nil {
		t.Error("FromManifests accepted a manifest with no source bytes")
	}
}
