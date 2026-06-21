// Package manifestpack implements out-of-band manifest packs (Epic MACFOR-MDC-4):
// a signed, versioned bundle of artifact manifests that can be loaded, verified,
// and constraint-checked independently of the macfor binary.
//
// A pack lets a release ship — or an operator side-load — updated artifact
// locations without rebuilding the tool, while preserving the trust guarantees
// the embedded manifests have. The package covers four concerns:
//
//   - Loading (MDC-4.1): read a pack directory, hash-verify every manifest file
//     against its declared SHA-256, and parse each manifest with the existing
//     artifacts parser. A mismatched hash is a tamper signal and is rejected.
//   - Signing & verification (MDC-4.2): an ed25519 detached signature over a
//     deterministic digest binds the pack version and every manifest's content
//     hash, so any change to any byte invalidates the signature.
//   - Provenance (MDC-4.3): a reproducible record of the pack version, overall
//     digest, and per-manifest hashes, suitable for recording in an evidence
//     container's chain of custody.
//   - Constraint enforcement (MDC-4.4): a pack carries DECLARATIVE config only.
//     Native parser references and raw SQL fragments (JOIN/WHERE/ORDER BY) are a
//     trust boundary (see pkg/macfor/tablemapper buildQuery) and are forbidden in
//     an out-of-band pack regardless of signature.
//
// On-disk layout (a directory):
//
//	<packdir>/
//	  pack.yaml                  // metadata (version, created_at, entries)
//	  manifests/<plugin.id>.yaml // one manifest file per plugin
//	  pack.sig                   // base64-encoded ed25519 detached signature
//
// ADOPTION NOTE: this package provides the loader, verifier, provenance, and
// constraints. Wiring a verified pack to OVERRIDE the embedded manifests during
// collection, and writing the pack provenance INTO the evidence container, are
// separate adoption steps that consume this package's API.
package manifestpack

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"gopkg.in/yaml.v3"
)

// PackFile is the metadata filename within a pack directory.
const PackFile = "pack.yaml"

// SigFile is the detached signature filename within a pack directory.
const SigFile = "pack.sig"

// Entry describes one manifest file inside a pack: which plugin it belongs to,
// its path relative to the pack root, and the SHA-256 of its raw bytes.
type Entry struct {
	// PluginID is the plugin identifier the manifest belongs to.
	PluginID string `yaml:"plugin_id"`

	// File is the manifest path relative to the pack root (e.g.
	// "manifests/browser.safari.yaml").
	File string `yaml:"file"`

	// SHA256 is the lowercase hex SHA-256 of the manifest file's raw bytes.
	SHA256 string `yaml:"sha256"`
}

// Pack is a loaded, hash-verified, constraint-checked manifest pack. The parsed
// manifests are keyed by plugin ID; the Entries slice preserves the on-disk
// metadata used to compute the digest and signature.
type Pack struct {
	// Version is the pack schema version (pack.yaml pack_version).
	Version string

	// CreatedAt is the informational creation timestamp (RFC3339).
	CreatedAt string

	// Entries are the per-manifest metadata records, as read from pack.yaml.
	Entries []Entry

	// Manifests are the parsed manifests keyed by plugin ID.
	Manifests map[string]*artifacts.Manifest
}

// packMeta is the on-disk shape of pack.yaml.
type packMeta struct {
	PackVersion string  `yaml:"pack_version"`
	CreatedAt   string  `yaml:"created_at"`
	Entries     []Entry `yaml:"entries"`
}

// Provenance is a reproducible record of a pack's identity (MDC-4.3): its
// version, overall digest, and per-manifest content hashes. It is the unit an
// adopter records into an evidence container's chain of custody so an analyst
// can later prove which manifest content drove a collection.
type Provenance struct {
	// PackVersion is the pack schema version.
	PackVersion string `json:"pack_version"`

	// Digest is the lowercase hex SHA-256 of the canonical signed payload (the
	// same bytes ed25519 signs). It binds the version and every manifest hash.
	Digest string `json:"digest"`

	// Entries are the per-manifest content hashes, sorted by plugin ID.
	Entries []ProvenanceEntry `json:"entries"`
}

// ProvenanceEntry records one manifest's plugin ID and content hash.
type ProvenanceEntry struct {
	// PluginID is the plugin identifier.
	PluginID string `json:"plugin_id"`

	// SHA256 is the lowercase hex SHA-256 of the manifest file bytes.
	SHA256 string `json:"sha256"`
}

// sqlIdentRe matches a bare SQL identifier: a leading letter or underscore
// followed by letters, digits, or underscores. This deliberately mirrors
// tablemapper's sqlIdentRe (we replicate the rule rather than import an internal
// symbol) so a pack cannot smuggle anything but a plain identifier into the FROM
// position or a column reference.
var sqlIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Load reads, hash-verifies, parses, and constraint-checks a manifest pack
// rooted at root within fsys (MDC-4.1). It does NOT verify the signature; call
// Verify for that. Accepting an fs.FS lets tests drive it with fstest.MapFS and
// callers drive it with os.DirFS.
//
// For each declared entry it reads the manifest bytes, recomputes their SHA-256,
// and REJECTS the pack if the hash differs from the entry's declared SHA256 —
// the on-disk tamper check. Each manifest is then parsed with the existing
// artifacts parser and run through enforceConstraints (MDC-4.4).
func Load(fsys fs.FS, root string) (*Pack, error) {
	metaBytes, err := fs.ReadFile(fsys, joinPath(root, PackFile))
	if err != nil {
		return nil, fmt.Errorf("manifestpack: reading %s: %w", PackFile, err)
	}

	var meta packMeta
	if err := yaml.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("manifestpack: parsing %s: %w", PackFile, err)
	}
	if meta.PackVersion == "" {
		return nil, errors.New("manifestpack: pack.yaml missing pack_version")
	}

	pack := &Pack{
		Version:   meta.PackVersion,
		CreatedAt: meta.CreatedAt,
		Entries:   meta.Entries,
		Manifests: make(map[string]*artifacts.Manifest, len(meta.Entries)),
	}

	seen := make(map[string]bool, len(meta.Entries))
	for _, e := range meta.Entries {
		if e.PluginID == "" {
			return nil, errors.New("manifestpack: entry missing plugin_id")
		}
		if seen[e.PluginID] {
			return nil, fmt.Errorf("manifestpack: duplicate plugin_id %q", e.PluginID)
		}
		seen[e.PluginID] = true

		if e.File == "" {
			return nil, fmt.Errorf("manifestpack: entry %q missing file", e.PluginID)
		}

		data, err := fs.ReadFile(fsys, joinPath(root, e.File))
		if err != nil {
			return nil, fmt.Errorf("manifestpack: reading manifest for %q: %w", e.PluginID, err)
		}

		// Tamper detection: the declared hash must match the bytes on disk.
		gotHash := hashHex(data)
		if !strings.EqualFold(gotHash, e.SHA256) {
			return nil, fmt.Errorf("manifestpack: hash mismatch for %q (%s): declared %s, computed %s",
				e.PluginID, e.File, e.SHA256, gotHash)
		}

		manifest, err := artifacts.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("manifestpack: parsing manifest for %q: %w", e.PluginID, err)
		}

		if err := enforceConstraints(manifest); err != nil {
			return nil, fmt.Errorf("manifestpack: constraint violation in %q: %w", e.PluginID, err)
		}

		pack.Manifests[e.PluginID] = manifest
	}

	return pack, nil
}

// FromManifests builds a Pack from already-parsed, TRUSTED manifests — the
// binary's own embedded set, gathered via the ManifestProvider registry — so the
// running collector can record the provenance of the manifests that drove a
// collection (the plugin remains the canonical source of truth; this only
// attests to what it was). No external pack file, no override.
//
// Unlike Load, FromManifests does NOT enforce pack constraints: embedded
// manifests are trusted and legitimately use native parsers, derive functions,
// and raw SQL. Each entry's content hash is the SHA-256 of a canonical YAML
// re-serialization of the parsed manifest — reproducible from the binary (not
// byte-identical to the original source file). version binds the digest to the
// tool/build version.
func FromManifests(version string, manifests map[string]*artifacts.Manifest) (*Pack, error) {
	pack := &Pack{
		Version:   version,
		Entries:   make([]Entry, 0, len(manifests)),
		Manifests: make(map[string]*artifacts.Manifest, len(manifests)),
	}

	ids := make([]string, 0, len(manifests))
	for id := range manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		m := manifests[id]
		if m == nil {
			return nil, fmt.Errorf("manifestpack: nil manifest for %q", id)
		}
		data, err := yaml.Marshal(m)
		if err != nil {
			return nil, fmt.Errorf("manifestpack: serializing manifest for %q: %w", id, err)
		}
		pack.Entries = append(pack.Entries, Entry{
			PluginID: id,
			File:     "manifests/" + id + ".yaml",
			SHA256:   hashHex(data),
		})
		pack.Manifests[id] = m
	}

	return pack, nil
}

// Digest returns a deterministic SHA-256 over a canonical serialization of the
// pack. The canonical payload sorts entries by plugin ID, then writes
// "<pack_version>\n" followed by "<plugin_id>\x00<sha256>\n" for each entry.
//
// This is the exact byte sequence that Sign signs and Verify checks. It binds
// the pack version together with every manifest's content hash, so changing the
// version, any manifest's bytes, the set of manifests, or their ordering all
// change the digest and therefore invalidate any signature.
func Digest(p *Pack) []byte {
	entries := make([]Entry, len(p.Entries))
	copy(entries, p.Entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].PluginID < entries[j].PluginID
	})

	var sb strings.Builder
	sb.WriteString(p.Version)
	sb.WriteByte('\n')
	for _, e := range entries {
		sb.WriteString(e.PluginID)
		sb.WriteByte(0)
		sb.WriteString(strings.ToLower(e.SHA256))
		sb.WriteByte('\n')
	}

	sum := sha256.Sum256([]byte(sb.String()))
	return sum[:]
}

// Sign returns the ed25519 detached signature of the pack's canonical digest
// (MDC-4.2). The returned raw signature is what verify checks; the on-disk
// pack.sig stores it base64-encoded.
func Sign(p *Pack, priv ed25519.PrivateKey) []byte {
	return ed25519.Sign(priv, Digest(p))
}

// Verify checks a base64-encoded detached signature against the pack's digest
// using pub (MDC-4.2). It returns a clear error — never a panic — for malformed
// base64, a wrong-length signature, a wrong-size key, or a failed cryptographic
// check.
func Verify(p *Pack, sigB64 string, pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("manifestpack: invalid public key size %d (want %d)", len(pub), ed25519.PublicKeySize)
	}

	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return fmt.Errorf("manifestpack: decoding signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("manifestpack: invalid signature size %d (want %d)", len(sig), ed25519.SignatureSize)
	}

	if !ed25519.Verify(pub, Digest(p), sig) {
		return errors.New("manifestpack: signature verification failed")
	}
	return nil
}

// BuildProvenance builds the reproducible provenance record for a pack
// (MDC-4.3): the pack version, hex digest, and per-entry content hashes sorted
// by plugin ID. (Named BuildProvenance because Go cannot share the identifier
// "Provenance" between this constructor and the Provenance type it returns.)
func BuildProvenance(p *Pack) Provenance {
	prov := Provenance{
		PackVersion: p.Version,
		Digest:      hex.EncodeToString(Digest(p)),
		Entries:     make([]ProvenanceEntry, 0, len(p.Entries)),
	}
	for _, e := range p.Entries {
		prov.Entries = append(prov.Entries, ProvenanceEntry{
			PluginID: e.PluginID,
			SHA256:   strings.ToLower(e.SHA256),
		})
	}
	sort.Slice(prov.Entries, func(i, j int) bool {
		return prov.Entries[i].PluginID < prov.Entries[j].PluginID
	})
	return prov
}

// enforceConstraints rejects a pack manifest that carries anything beyond
// declarative configuration (MDC-4.4). An out-of-band pack is an untrusted input
// to a forensic tool, so it must never be able to:
//
//   - reference a native parser (Artifact.Parser) or a field-level derive
//     function (FieldSpec.Derived) — both point the runtime at compiled-in code
//     chosen by an external file;
//   - supply raw SQL fragments (Extract.Join / Where / OrderBy) — these are
//     interpolated verbatim by tablemapper.buildQuery and are a documented trust
//     boundary; or
//   - use anything but a bare identifier for Extract.From, or anything but a
//     bare / "table.column" identifier for a field Column — the same rule
//     tablemapper.validateColumnRef enforces, replicated here so a pack is
//     screened before it ever reaches the query builder.
//
// Errors name the offending artifact so an operator can pinpoint the violation.
func enforceConstraints(m *artifacts.Manifest) error {
	if m == nil {
		return errors.New("nil manifest")
	}

	for i := range m.Artifacts {
		a := &m.Artifacts[i]

		if a.Parser != "" {
			return fmt.Errorf("artifact %q references native parser %q: packs may not reference native parser code",
				a.ID, a.Parser)
		}

		if a.Extract == nil {
			continue
		}
		ext := a.Extract

		if ext.Join != "" {
			return fmt.Errorf("artifact %q has extract.join: raw SQL fragments are forbidden in packs", a.ID)
		}
		if ext.Where != "" {
			return fmt.Errorf("artifact %q has extract.where: raw SQL fragments are forbidden in packs", a.ID)
		}
		if ext.OrderBy != "" {
			return fmt.Errorf("artifact %q has extract.order_by: raw SQL fragments are forbidden in packs", a.ID)
		}

		// From is only meaningful for tabular extracts; when set it must be a bare
		// identifier (plist extracts use Records instead and leave From empty).
		if ext.From != "" {
			if err := validateIdentifier(ext.From); err != nil {
				return fmt.Errorf("artifact %q has invalid extract.from: %w", a.ID, err)
			}
		}

		for j := range ext.Fields {
			f := &ext.Fields[j]

			// A derived field selects a compiled-in derive function by name, so it
			// is native-code selection just like Parser — forbidden in a pack.
			if f.Derived != "" {
				return fmt.Errorf("artifact %q field %q references derive function %q: packs may not select native derive code",
					a.ID, f.Name, f.Derived)
			}

			if f.Column == "" {
				continue
			}
			if err := validateColumnRef(f.Column); err != nil {
				return fmt.Errorf("artifact %q field %q has invalid column: %w", a.ID, f.Name, err)
			}
		}
	}

	return nil
}

// validateIdentifier returns an error if s is not a bare SQL identifier. It
// mirrors tablemapper.validateIdentifier.
func validateIdentifier(s string) error {
	if !sqlIdentRe.MatchString(s) {
		return fmt.Errorf("invalid SQL identifier %q", s)
	}
	return nil
}

// validateColumnRef returns an error if s is not a bare identifier or a
// table-qualified identifier ("table.column"). It mirrors
// tablemapper.validateColumnRef.
func validateColumnRef(s string) error {
	for _, part := range strings.Split(s, ".") {
		if err := validateIdentifier(part); err != nil {
			return fmt.Errorf("invalid column reference %q: %w", s, err)
		}
	}
	if strings.Count(s, ".") > 1 {
		return fmt.Errorf("invalid column reference %q: too many qualifiers", s)
	}
	return nil
}

// hashHex returns the lowercase hex SHA-256 of data.
func hashHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// joinPath joins a pack root and a relative member using forward slashes, the
// separator io/fs requires. An empty or "." root yields the member unchanged.
func joinPath(root, rel string) string {
	if root == "" || root == "." {
		return rel
	}
	return path.Join(root, rel)
}
