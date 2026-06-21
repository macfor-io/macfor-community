package live

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// dsclTimeout is the maximum time to wait for a dscl command to complete.
const dsclTimeout = 10 * time.Second

// dslocalUsersPath is the directory containing user plists on macOS.
const dslocalUsersPath = "/var/db/dslocal/nodes/Default/users"

// passwdPath is the fallback path for user enumeration.
const passwdPath = "/etc/passwd"

// minHumanUID is the minimum UID for human users on macOS.
// System and service accounts have UIDs below this threshold.
const minHumanUID = 500

// Non-interactive login shells that indicate a service or system account.
const (
	shellUsrBinFalse = "/usr/bin/false"
	shellSbinNologin = "/sbin/nologin"
	shellBinFalse    = "/bin/false"
	usernameNobody   = "nobody"
)

// serviceShells are shells that indicate a service or system account.
var serviceShells = map[string]bool{
	shellUsrBinFalse: true,
	shellSbinNologin: true,
	shellBinFalse:    true,
}

// userPlist represents the structure of a user plist file in dslocal.
// Only the fields we need are included.
type userPlist struct {
	// Name is the username (login name).
	// The plist stores this as an array, but typically has only one element.
	Name []string `plist:"name"`

	// RealName is the user's full display name.
	RealName []string `plist:"realname"`

	// UniqueID is the numeric user ID.
	UniqueID []string `plist:"uid"`

	// PrimaryGroupID is the primary group ID.
	PrimaryGroupID []string `plist:"gid"`

	// NFSHomeDirectory is the path to the user's home directory.
	NFSHomeDirectory []string `plist:"home"`

	// UserShell is the user's login shell.
	UserShell []string `plist:"shell"`
}

// Users returns all human user accounts on the live system.
// It tries multiple methods to enumerate users:
//  1. Direct access to dslocal plists (requires root)
//  2. dscl command-line tool (works for non-root)
//  3. Fallback to /etc/passwd (incomplete on macOS)
//
// On macOS, user data is stored in plist files under
// /var/db/dslocal/nodes/Default/users/. If this directory is not
// accessible (e.g., running without elevated privileges), it falls
// back to using dscl or parsing /etc/passwd.
func (s *Source) Users() ([]macfor.User, error) {
	// Try dslocal first (requires root)
	users, err := s.usersFromDSLocal()
	if err == nil && len(users) > 0 {
		return users, nil
	}

	// Try dscl command (works without root on macOS)
	users, err = s.usersFromDSCL()
	if err == nil && len(users) > 0 {
		return users, nil
	}

	// Fall back to /etc/passwd if everything else fails
	return s.usersFromPasswd()
}

// usersFromDSLocal enumerates users from the dslocal database.
func (s *Source) usersFromDSLocal() ([]macfor.User, error) {
	entries, err := s.ReadDir(dslocalUsersPath)
	if err != nil {
		return nil, fmt.Errorf("reading dslocal users directory: %w", err)
	}

	var users []macfor.User
	for _, entry := range entries {
		// Skip directories and non-plist files
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".plist") {
			continue
		}

		plistPath := dslocalUsersPath + "/" + entry.Name()
		user, err := s.parseUserPlist(plistPath)
		if err != nil {
			// Log and continue - don't fail on individual user parsing errors
			continue
		}

		if isHumanUser(user) {
			users = append(users, user)
		}
	}

	return users, nil
}

// parseUserPlist parses a single user plist file.
func (s *Source) parseUserPlist(path string) (macfor.User, error) {
	var up userPlist
	if err := s.ReadPlist(path, &up); err != nil {
		return macfor.User{}, fmt.Errorf("parsing user plist %s: %w", path, err)
	}

	// Extract first element from each array field (macOS stores these as arrays)
	user := macfor.User{
		Username: firstString(up.Name),
		Name:     firstString(up.RealName),
		HomeDir:  firstString(up.NFSHomeDirectory),
		Shell:    firstString(up.UserShell),
	}

	// Parse UID
	if uidStr := firstString(up.UniqueID); uidStr != "" {
		if uid, err := strconv.Atoi(uidStr); err == nil {
			user.UID = uid
		}
	}

	// Parse GID
	if gidStr := firstString(up.PrimaryGroupID); gidStr != "" {
		if gid, err := strconv.Atoi(gidStr); err == nil {
			user.GID = gid
		}
	}

	return user, nil
}

// usersFromDSCL uses the dscl command-line tool to enumerate users.
// This works without root access on macOS and provides full user information
// from Open Directory.
func (s *Source) usersFromDSCL() ([]macfor.User, error) {
	// Get list of all users
	ctx, cancel := context.WithTimeout(context.Background(), dsclTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dscl", ".", "-list", "/Users")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running dscl list: %w", err)
	}

	var users []macfor.User
	scanner := bufio.NewScanner(bytes.NewReader(output))

	for scanner.Scan() {
		username := strings.TrimSpace(scanner.Text())
		if username == "" {
			continue
		}

		// Get detailed info for this user
		user := s.getUserFromDSCL(username)

		if isHumanUser(user) {
			users = append(users, user)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning dscl output: %w", err)
	}

	return users, nil
}

// getUserFromDSCL retrieves detailed user information using dscl.
func (s *Source) getUserFromDSCL(username string) macfor.User {
	userPath := "/Users/" + username

	user := macfor.User{
		Username: username,
	}

	// Get UID
	if uid, err := s.dsclReadAttr(userPath, "UniqueID"); err == nil && uid != "" {
		if uidInt, err := strconv.Atoi(uid); err == nil {
			user.UID = uidInt
		}
	}

	// Get GID
	if gid, err := s.dsclReadAttr(userPath, "PrimaryGroupID"); err == nil && gid != "" {
		if gidInt, err := strconv.Atoi(gid); err == nil {
			user.GID = gidInt
		}
	}

	// Get real name
	if name, err := s.dsclReadAttr(userPath, "RealName"); err == nil {
		user.Name = name
	}

	// Get home directory
	if home, err := s.dsclReadAttr(userPath, "NFSHomeDirectory"); err == nil {
		user.HomeDir = home
	}

	// Get shell
	if shell, err := s.dsclReadAttr(userPath, "UserShell"); err == nil {
		user.Shell = shell
	}

	return user
}

// dsclReadAttr reads a single attribute from dscl.
func (s *Source) dsclReadAttr(path, attr string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dsclTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dscl", ".", "-read", path, attr)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Output format is "AttrName: value" or "AttrName:\n value" for multi-line
	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("empty output")
	}

	// Check first line for "AttrName: value" format
	firstLine := lines[0]
	if idx := strings.Index(firstLine, ":"); idx >= 0 {
		value := strings.TrimSpace(firstLine[idx+1:])
		if value != "" {
			return value, nil
		}
		// Value might be on the next line (multi-line format)
		if len(lines) > 1 {
			return strings.TrimSpace(lines[1]), nil
		}
	}

	return "", fmt.Errorf("could not parse dscl output")
}

// usersFromPasswd parses /etc/passwd as a fallback.
// Note: On macOS, /etc/passwd is incomplete and doesn't contain all users.
func (s *Source) usersFromPasswd() ([]macfor.User, error) {
	f, err := s.Open(passwdPath)
	if err != nil {
		return nil, fmt.Errorf("opening passwd file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var users []macfor.User
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		user, err := parsePasswdLine(line)
		if err != nil {
			// Skip malformed lines
			continue
		}

		if isHumanUser(user) {
			users = append(users, user)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading passwd file: %w", err)
	}

	return users, nil
}

// parsePasswdLine parses a single line from /etc/passwd.
// Format: username:password:uid:gid:gecos:home:shell.
func parsePasswdLine(line string) (macfor.User, error) {
	fields := strings.Split(line, ":")
	if len(fields) < 7 {
		return macfor.User{}, fmt.Errorf("invalid passwd line: expected 7 fields, got %d", len(fields))
	}

	uid, err := strconv.Atoi(fields[2])
	if err != nil {
		return macfor.User{}, fmt.Errorf("invalid UID: %w", err)
	}

	gid, err := strconv.Atoi(fields[3])
	if err != nil {
		return macfor.User{}, fmt.Errorf("invalid GID: %w", err)
	}

	return macfor.User{
		Username: fields[0],
		UID:      uid,
		GID:      gid,
		Name:     fields[4], // GECOS field contains full name
		HomeDir:  fields[5],
		Shell:    fields[6],
	}, nil
}

// isHumanUser returns true if the user appears to be a human user account
// rather than a system or service account.
func isHumanUser(u macfor.User) bool {
	// Filter by UID - system accounts have UIDs below minHumanUID
	if u.UID < minHumanUID {
		return false
	}

	// Filter by shell - service accounts use nologin or false
	if serviceShells[u.Shell] {
		return false
	}

	// Filter out special system users that might have high UIDs
	// (e.g., nobody is UID 65534 on some systems)
	if u.Username == usernameNobody || u.Username == "nfsnobody" {
		return false
	}

	return true
}

// firstString returns the first element of a string slice, or empty string if empty.
func firstString(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}
