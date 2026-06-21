package cli

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/macfor-io/macfor-community/pkg/macfor/manifestpack"
	"github.com/spf13/cobra"
)

// trustedPublicKeyHex is the ed25519 public key (hex-encoded) embedded by a
// release to verify its official manifest packs. It is intentionally empty in
// source: a release build sets this to its key so `manifest pack verify` works
// with no --pubkey flag. When empty, verification REQUIRES --pubkey and errors
// clearly if it is absent.
var trustedPublicKeyHex = ""

// Manifest pack command flags.
var (
	packPubKey  string
	packKeyFile string
)

// newManifestPackCmd creates the `manifest pack` parent command and its
// verify / info / sign subcommands.
func newManifestPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Work with out-of-band manifest packs",
		Long: `Load, verify, and inspect out-of-band manifest packs.

A manifest pack is a signed, versioned directory of artifact manifests that can
be distributed and verified independently of the macfor binary. Packs carry
declarative configuration only: native parser references and raw SQL fragments
are rejected at load time as a trust boundary.`,
		Run: func(cmd *cobra.Command, _ []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(newManifestPackVerifyCmd())
	cmd.AddCommand(newManifestPackInfoCmd())
	cmd.AddCommand(newManifestPackSignCmd())

	return cmd
}

// newManifestPackVerifyCmd creates the `manifest pack verify` subcommand.
func newManifestPackVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <packdir>",
		Short: "Load and cryptographically verify a manifest pack",
		Long: `Load a manifest pack, verify every manifest's content hash and the pack's
ed25519 signature, enforce the declarative-only constraints, and print the
pack's provenance on success.

The public key is taken from --pubkey (a hex string or a path to a file
containing the hex key). If omitted, the binary's embedded trusted key is used;
when no key is embedded, --pubkey is required.

Examples:
  macfor manifest pack verify ./pack
  macfor manifest pack verify ./pack --pubkey ./release.pub
  macfor manifest pack verify ./pack --pubkey 3b6a...`,
		Args: cobra.ExactArgs(1),
		RunE: runManifestPackVerify,
	}
	cmd.Flags().StringVar(&packPubKey, "pubkey", "", "ed25519 public key as hex, or a path to a file containing the hex key")
	return cmd
}

// newManifestPackInfoCmd creates the `manifest pack info` subcommand.
func newManifestPackInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <packdir>",
		Short: "Load and describe a manifest pack (no signature check)",
		Long: `Load a manifest pack without checking its signature and print its version,
creation time, and each plugin's manifest hash. Constraint enforcement still
runs, so declarative-only violations are reported.

Examples:
  macfor manifest pack info ./pack`,
		Args: cobra.ExactArgs(1),
		RunE: runManifestPackInfo,
	}
	return cmd
}

// newManifestPackSignCmd creates the `manifest pack sign` subcommand.
func newManifestPackSignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sign <packdir>",
		Short: "Sign a manifest pack (dev/release tooling)",
		Long: `Compute a pack's canonical digest, sign it with an ed25519 private key, and
write the detached base64 signature to pack.sig in the pack directory.

The private key is read from --key (a path to a file containing the hex-encoded
ed25519 private key).

Examples:
  macfor manifest pack sign ./pack --key ./release.key`,
		Args: cobra.ExactArgs(1),
		RunE: runManifestPackSign,
	}
	cmd.Flags().StringVar(&packKeyFile, "key", "", "path to a file containing the hex-encoded ed25519 private key")
	_ = cmd.MarkFlagRequired("key")
	return cmd
}

// runManifestPackVerify loads a pack, verifies its signature, and prints
// provenance on success.
func runManifestPackVerify(cmd *cobra.Command, args []string) error {
	packDir := args[0]

	pack, err := manifestpack.Load(os.DirFS(packDir), ".")
	if err != nil {
		return err
	}

	pub, err := resolvePublicKey()
	if err != nil {
		return err
	}

	sigB64, err := readSignature(packDir)
	if err != nil {
		return err
	}

	if err := manifestpack.Verify(pack, sigB64, pub); err != nil {
		return err
	}

	prov := manifestpack.BuildProvenance(pack)
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "OK: signature verified\n")
	_, _ = fmt.Fprintf(out, "pack_version: %s\n", prov.PackVersion)
	_, _ = fmt.Fprintf(out, "digest:       %s\n", prov.Digest)
	_, _ = fmt.Fprintf(out, "manifests:    %d\n", len(prov.Entries))
	for _, e := range prov.Entries {
		_, _ = fmt.Fprintf(out, "  %s  %s\n", e.SHA256, e.PluginID)
	}
	return nil
}

// runManifestPackInfo loads a pack without signature verification and prints its
// metadata and per-manifest hashes.
func runManifestPackInfo(cmd *cobra.Command, args []string) error {
	packDir := args[0]

	pack, err := manifestpack.Load(os.DirFS(packDir), ".")
	if err != nil {
		return err
	}

	prov := manifestpack.BuildProvenance(pack)
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "pack_version: %s\n", pack.Version)
	_, _ = fmt.Fprintf(out, "created_at:   %s\n", pack.CreatedAt)
	_, _ = fmt.Fprintf(out, "digest:       %s\n", prov.Digest)
	_, _ = fmt.Fprintf(out, "manifests:    %d\n", len(prov.Entries))
	for _, e := range prov.Entries {
		_, _ = fmt.Fprintf(out, "  %s  %s\n", e.SHA256, e.PluginID)
	}
	return nil
}

// runManifestPackSign loads a pack, signs its digest with the supplied private
// key, and writes the base64 signature to pack.sig.
func runManifestPackSign(cmd *cobra.Command, args []string) error {
	packDir := args[0]

	pack, err := manifestpack.Load(os.DirFS(packDir), ".")
	if err != nil {
		return err
	}

	priv, err := readPrivateKey(packKeyFile)
	if err != nil {
		return err
	}

	sig := manifestpack.Sign(pack, priv)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	sigPath := filepath.Join(packDir, manifestpack.SigFile)
	if err := os.WriteFile(sigPath, []byte(sigB64+"\n"), 0o644); err != nil { //nolint:gosec // Signature is not secret.
		return fmt.Errorf("writing signature: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote signature to %s\n", sigPath)
	return nil
}

// resolvePublicKey returns the ed25519 public key to verify with: --pubkey (hex
// string or path to a hex file) takes precedence, otherwise the embedded trusted
// key. It errors clearly when neither is available.
func resolvePublicKey() (ed25519.PublicKey, error) {
	hexKey := packPubKey
	if hexKey == "" {
		if trustedPublicKeyHex == "" {
			return nil, errors.New("no public key: pass --pubkey (this build has no embedded trusted key)")
		}
		hexKey = trustedPublicKeyHex
	} else if isExistingFile(hexKey) {
		data, err := os.ReadFile(hexKey) //nolint:gosec // Key path is from user CLI input.
		if err != nil {
			return nil, fmt.Errorf("reading public key file: %w", err)
		}
		hexKey = strings.TrimSpace(string(data))
	}

	raw, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil {
		return nil, fmt.Errorf("decoding public key hex: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size %d (want %d)", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// readPrivateKey reads a hex-encoded ed25519 private key from a file.
func readPrivateKey(file string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(file) //nolint:gosec // Key path is from user CLI input.
	if err != nil {
		return nil, fmt.Errorf("reading private key file: %w", err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decoding private key hex: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size %d (want %d)", len(raw), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}

// readSignature reads the base64 detached signature from a pack's pack.sig.
func readSignature(packDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(packDir, manifestpack.SigFile)) //nolint:gosec // Path derived from CLI arg.
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", manifestpack.SigFile, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// isExistingFile reports whether path names an existing regular file.
func isExistingFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
