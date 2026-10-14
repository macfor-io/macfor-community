package users

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// typeUserAccount is the record type for a parsed local account.
const typeUserAccount = "user_account"

// artifactKindAccounts is the macfor artifact type for account records. It
// keys the Parquet schema registration.
const artifactKindAccounts = "user_accounts"

// categoryAccounts is the evidence category for account records.
const categoryAccounts = "system"

// minHumanUID is the lowest UID macOS assigns to accounts created for people.
const minHumanUID = 500

// serviceShells are login shells that disable interactive login.
var serviceShells = map[string]bool{
	"/usr/bin/false":    true,
	"/bin/false":        true,
	"/sbin/nologin":     true,
	"/usr/sbin/nologin": true,
}

// authority markers found in authentication_authority values.
const (
	authoritySecureToken = ";SecureToken;"
	authorityShadowHash  = ";ShadowHash;"
)

// AccountRecord is the metadata recorded for one local account. It holds no
// credential material: password hashes, Kerberos keys, and the password hint
// are never read from the source plist.
type AccountRecord struct {
	// Type identifies this as a user account record.
	Type string `json:"type"`

	// Username is the account's short (login) name.
	Username string `json:"username"`

	// Aliases are additional record names for the account, such as an
	// email-style alias.
	Aliases []string `json:"aliases,omitempty"`

	// RealName is the account's full display name.
	RealName string `json:"real_name,omitempty"`

	// UID is the numeric user ID (nil if absent or not numeric).
	UID *int64 `json:"uid,omitempty"`

	// GID is the primary group ID (nil if absent or not numeric).
	GID *int64 `json:"gid,omitempty"`

	// GeneratedUID is the account's directory services GUID.
	GeneratedUID string `json:"generated_uid,omitempty"`

	// HomeDir is the account's home directory.
	HomeDir string `json:"home_dir,omitempty"`

	// Shell is the account's login shell.
	Shell string `json:"shell,omitempty"`

	// Hidden reports whether the account is hidden from the login window.
	Hidden bool `json:"hidden"`

	// ServiceAccount reports whether the account looks like a system or
	// service account (UID below 500 or a non-interactive shell) rather than
	// one created for a person.
	ServiceAccount bool `json:"service_account"`

	// HasPassword reports whether the account authenticates with a local
	// password hash. Only the presence of the authority is recorded.
	HasPassword bool `json:"has_password"`

	// SecureToken reports whether the account holds a Secure Token, which
	// lets it unlock FileVault.
	SecureToken bool `json:"secure_token"`

	// CreationTime is when the account was created.
	CreationTime *time.Time `json:"creation_time,omitempty"`

	// PasswordLastSetTime is when the account's password last changed.
	PasswordLastSetTime *time.Time `json:"password_last_set_time,omitempty"`

	// FailedLoginCount is the number of failed logins since the last success.
	FailedLoginCount *int64 `json:"failed_login_count,omitempty"`

	// FailedLoginTime is when the most recent failed login happened.
	FailedLoginTime *time.Time `json:"failed_login_time,omitempty"`

	// SourceFile is the path to the account plist this record was parsed from.
	SourceFile string `json:"source_file"`

	// SourceSHA256 is the SHA-256 of the account plist. The raw file is not
	// collected because it holds credential material, so the hash lets an
	// analyst verify the record against the original.
	SourceSHA256 string `json:"source_sha256"`
}

// dslocalAccount is the allowlist of keys read from an account plist. Every
// value is stored as an array. Keys not listed here, including every key that
// holds credential material, are never decoded.
type dslocalAccount struct {
	Name                    []string `plist:"name"`
	RealName                []string `plist:"realname"`
	UID                     []string `plist:"uid"`
	GID                     []string `plist:"gid"`
	GeneratedUID            []string `plist:"generateduid"`
	Home                    []string `plist:"home"`
	Shell                   []string `plist:"shell"`
	IsHidden                []string `plist:"IsHidden"`
	AuthenticationAuthority []string `plist:"authentication_authority"`
	AccountPolicyData       [][]byte `plist:"accountPolicyData"`
}

// accountPolicy is the allowlist of keys read from the embedded
// accountPolicyData property list. Times are seconds since the Unix epoch.
type accountPolicy struct {
	CreationTime         float64 `plist:"creationTime"`
	PasswordLastSetTime  float64 `plist:"passwordLastSetTime"`
	FailedLoginCount     *int64  `plist:"failedLoginCount"`
	FailedLoginTimestamp float64 `plist:"failedLoginTimestamp"`
}

// accountPlistPaths returns the source paths of every account plist. Go's glob
// ignores directory read errors, so each glob's directory is read first: on a
// live system without root that surfaces the permission error instead of
// reporting no accounts.
func accountPlistPaths(src macfor.Source) ([]string, error) {
	resolved, err := locator.Paths(src, systemUser, string(ArtifactLocalAccounts))
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, rp := range resolved {
		if rp.Type != artifacts.PathTypeGlob {
			paths = append(paths, rp.Path)
			continue
		}

		if _, err := src.ReadDir(path.Dir(rp.Path)); err != nil {
			if isNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading account directory: %w", err)
		}

		matches, err := src.Glob(rp.Path)
		if err != nil {
			return nil, fmt.Errorf("listing account plists: %w", err)
		}
		paths = append(paths, matches...)
	}

	return paths, nil
}

// collectLocalAccounts parses every account plist into an AccountRecord. A
// plist that cannot be read or parsed is recorded as an error and skipped.
func collectLocalAccounts(ctx context.Context, src macfor.Source, out macfor.Collector) error {
	paths, err := accountPlistPaths(src)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		out.Log(macfor.LogInfo, "No local account plists found")
		return nil
	}

	out.Log(macfor.LogInfo, "Parsing %d local account plist(s)", len(paths))

	var parseErrors []error
	count := 0
	for _, p := range paths {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		artifact, err := macfor.NewArtifact(PluginID, categoryAccounts, artifactKindAccounts)
		if err != nil {
			return fmt.Errorf("creating artifact: %w", err)
		}
		artifact.WithSourcePath(p).WithDescription("Local user account metadata")

		record, err := readAccount(src, p)
		if err != nil {
			out.AddError(*artifact, err)
			parseErrors = append(parseErrors, err)
			continue
		}

		artifact.WithUsername(record.Username)
		if err := out.AddRecord(ctx, *artifact, record); err != nil {
			out.AddError(*artifact, fmt.Errorf("adding record for %s: %w", record.Username, err))
			continue
		}
		count++
	}

	out.Log(macfor.LogInfo, "Collected %d local account(s)", count)

	if len(parseErrors) > 0 {
		return errors.Join(parseErrors...)
	}
	return nil
}

// readAccount reads and parses one account plist.
func readAccount(src macfor.Source, p string) (AccountRecord, error) {
	f, err := src.Open(p)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("opening %s: %w", p, err)
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return AccountRecord{}, fmt.Errorf("reading %s: %w", p, err)
	}

	record, err := parseAccount(data)
	if err != nil {
		return AccountRecord{}, fmt.Errorf("parsing %s: %w", p, err)
	}

	sum := sha256.Sum256(data)
	record.SourceFile = p
	record.SourceSHA256 = hex.EncodeToString(sum[:])
	return record, nil
}

// parseAccount decodes the allowlisted keys of an account plist into a record.
func parseAccount(data []byte) (AccountRecord, error) {
	var acct dslocalAccount
	if _, err := plist.Unmarshal(data, &acct); err != nil {
		return AccountRecord{}, err
	}
	if len(acct.Name) == 0 || acct.Name[0] == "" {
		return AccountRecord{}, fmt.Errorf("account plist has no name")
	}

	record := AccountRecord{
		Type:         typeUserAccount,
		Username:     acct.Name[0],
		RealName:     first(acct.RealName),
		UID:          parseID(first(acct.UID)),
		GID:          parseID(first(acct.GID)),
		GeneratedUID: first(acct.GeneratedUID),
		HomeDir:      first(acct.Home),
		Shell:        first(acct.Shell),
		Hidden:       isTrue(first(acct.IsHidden)),
	}
	if len(acct.Name) > 1 {
		record.Aliases = acct.Name[1:]
	}

	record.ServiceAccount = record.UID == nil || *record.UID < minHumanUID || serviceShells[record.Shell]

	for _, a := range acct.AuthenticationAuthority {
		if strings.Contains(a, authorityShadowHash) {
			record.HasPassword = true
		}
		if strings.Contains(a, authoritySecureToken) {
			record.SecureToken = true
		}
	}

	if len(acct.AccountPolicyData) > 0 {
		applyAccountPolicy(&record, acct.AccountPolicyData[0])
	}

	return record, nil
}

// applyAccountPolicy copies the account timestamps and failed-login count from
// embedded accountPolicyData. Malformed data leaves the fields unset.
func applyAccountPolicy(record *AccountRecord, data []byte) {
	var policy accountPolicy
	if _, err := plist.Unmarshal(bytes.TrimSpace(data), &policy); err != nil {
		return
	}
	record.CreationTime = unixTime(policy.CreationTime)
	record.PasswordLastSetTime = unixTime(policy.PasswordLastSetTime)
	record.FailedLoginTime = unixTime(policy.FailedLoginTimestamp)
	record.FailedLoginCount = policy.FailedLoginCount
}

// unixTime converts fractional seconds since the Unix epoch to a UTC time.
// Zero means the value is absent.
func unixTime(secs float64) *time.Time {
	if secs <= 0 || math.IsNaN(secs) || math.IsInf(secs, 0) {
		return nil
	}
	whole, frac := math.Modf(secs)
	t := time.Unix(int64(whole), int64(frac*1e9)).UTC()
	return &t
}

// parseID parses a numeric ID, returning nil when it is absent or invalid.
func parseID(s string) *int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// isTrue reports whether a directory services boolean string is set.
func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// first returns the first element of a string slice, or "" if it is empty.
func first(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

// isNotExist reports whether err means a path does not exist.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, macfor.ErrNotFound)
}
