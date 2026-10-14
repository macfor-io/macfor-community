package directory

import (
	"os"
	"strconv"
	"strings"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Users returns all human user accounts found in the directory tree.
// It scans the (merged) Users/ directory for home directories and supplements
// each entry with metadata from dslocal node plists when available.
func (s *Source) Users() ([]macfor.User, error) {
	usersPath, err := joinRoot(s.root, "Users")
	if err != nil {
		return nil, err
	}

	// List Users/ merged across all roots: on a 10.15+ System volume Users/ is
	// an empty firmlink placeholder and the home directories live on Data.
	entries, err := s.readDirMerged("Users")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, macfor.WrapIOError("readdir", usersPath, err)
	}

	var users []macfor.User
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip Shared and hidden directories.
		if name == "Shared" || strings.HasPrefix(name, ".") {
			continue
		}

		user := macfor.User{
			Username: name,
			HomeDir:  "/Users/" + name,
		}

		// Try to supplement with dslocal plist metadata.
		s.enrichUserFromDSLocal(&user, name)

		users = append(users, user)
	}

	return users, nil
}

// enrichUserFromDSLocal attempts to read UID, GID, Name, and Shell from the dslocal plist.
// Failures are silently ignored — the user is returned with whatever fields could be read.
func (s *Source) enrichUserFromDSLocal(user *macfor.User, username string) {
	plistPath := "private/var/db/dslocal/nodes/Default/users/" + username + ".plist"

	var up dsLocalUserPlist
	if err := s.ReadPlist(plistPath, &up); err != nil {
		return
	}

	if uid := firstStr(up.UID); uid != "" {
		if n, err := strconv.Atoi(uid); err == nil {
			user.UID = n
		}
	}
	if gid := firstStr(up.GID); gid != "" {
		if n, err := strconv.Atoi(gid); err == nil {
			user.GID = n
		}
	}
	user.Name = firstStr(up.RealName)
	user.Shell = firstStr(up.Shell)
}

// dsLocalUserPlist represents the user plist structure in dslocal.
// Each field is stored as an array of strings (macOS dslocal format).
type dsLocalUserPlist struct {
	Name     []string `plist:"name"`
	RealName []string `plist:"realname"`
	UID      []string `plist:"uid"`
	GID      []string `plist:"gid"`
	Home     []string `plist:"home"`
	Shell    []string `plist:"shell"`
}

// firstStr returns the first element of a string slice, or empty string.
func firstStr(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}
