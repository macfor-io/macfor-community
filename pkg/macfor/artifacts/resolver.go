package artifacts

import (
	"errors"
	"path/filepath"
	"strings"
)

// Sentinel errors for resolver operations.
var (
	// ErrNoMatchingPath indicates no path matched the OS version constraints.
	ErrNoMatchingPath = errors.New("no matching path for OS version")

	// ErrArtifactUnavailable indicates the artifact is not available on this OS version.
	ErrArtifactUnavailable = errors.New("artifact not available on this OS version")
)

// Resolver handles path expansion and version-aware resolution.
type Resolver struct {
	osVersion  string
	appVersion string
	rootPath   string // For disk image support: "/mnt/image".
}

// NewResolver creates a resolver for the given OS version.
func NewResolver(osVersion string) *Resolver {
	return &Resolver{
		osVersion: osVersion,
		rootPath:  "",
	}
}

// WithRootPath sets a root path prefix for disk image analysis.
// All resolved paths will be prefixed with this path.
func (r *Resolver) WithRootPath(root string) *Resolver {
	r.rootPath = root
	return r
}

// WithAppVersion sets the application version for app-specific paths.
func (r *Resolver) WithAppVersion(version string) *Resolver {
	r.appVersion = version
	return r
}

// ResolvedPath represents a fully resolved, absolute path.
type ResolvedPath struct {
	// Path is the resolved absolute path.
	Path string

	// Type is the path type: file, glob, or directory.
	Type string

	// Artifact is the artifact ID this path belongs to.
	Artifact string

	// User is the username this path was resolved for.
	User string
}

// UserInfo provides the minimum user information needed for path resolution.
// This is an interface to avoid circular imports with the macfor package.
type UserInfo interface {
	// GetUsername returns the user's login name.
	GetUsername() string

	// GetHomeDir returns the user's home directory path.
	GetHomeDir() string
}

// SimpleUser is a basic implementation of UserInfo for testing and simple use cases.
type SimpleUser struct {
	Username string
	HomeDir  string
}

// GetUsername returns the username.
func (u SimpleUser) GetUsername() string {
	return u.Username
}

// GetHomeDir returns the home directory.
func (u SimpleUser) GetHomeDir() string {
	return u.HomeDir
}

// ResolveArtifact returns all applicable paths for an artifact and user.
// For optional artifacts, an empty slice is returned if no paths match.
// For required artifacts, ErrNoMatchingPath is returned if no paths match.
func (r *Resolver) ResolveArtifact(artifact Artifact, user UserInfo) ([]ResolvedPath, error) {
	var resolved []ResolvedPath

	for _, pathSpec := range artifact.Paths {
		if !r.pathMatchesVersion(pathSpec) {
			continue
		}

		expanded := r.expandPath(pathSpec.Pattern, user)
		pathType := pathSpec.Type
		if pathType == "" {
			pathType = PathTypeFile
		}

		resolved = append(resolved, ResolvedPath{
			Path:     expanded,
			Type:     pathType,
			Artifact: artifact.ID,
			User:     user.GetUsername(),
		})
	}

	if len(resolved) == 0 && !artifact.Optional {
		return nil, ErrNoMatchingPath
	}

	return resolved, nil
}

// pathMatchesVersion checks if a PathSpec applies to the current OS/app version.
func (r *Resolver) pathMatchesVersion(p PathSpec) bool {
	// Check OS version constraints.
	if p.OSMin != "" && CompareVersions(r.osVersion, p.OSMin) < 0 {
		return false
	}
	if p.OSMax != "" && CompareVersions(r.osVersion, p.OSMax) > 0 {
		return false
	}

	// Check app version constraints (only if both are specified).
	if p.AppMin != "" && r.appVersion != "" && CompareVersions(r.appVersion, p.AppMin) < 0 {
		return false
	}
	if p.AppMax != "" && r.appVersion != "" && CompareVersions(r.appVersion, p.AppMax) > 0 {
		return false
	}

	return true
}

// expandPath replaces tokens with actual values.
func (r *Resolver) expandPath(pattern string, user UserInfo) string {
	path := pattern

	// Expand ~ to user home directory.
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(user.GetHomeDir(), path[2:])
	} else if path == "~" {
		path = user.GetHomeDir()
	}

	// Expand {username} token.
	path = strings.ReplaceAll(path, "{username}", user.GetUsername())

	// Apply root path for disk image analysis.
	if r.rootPath != "" {
		// Convert absolute path to rooted path.
		// /Users/admin/.zsh_history → /mnt/image/Users/admin/.zsh_history.
		if filepath.IsAbs(path) {
			path = filepath.Join(r.rootPath, path)
		}
	}

	return path
}

// ResolveRelated expands related file patterns for an artifact.
func (r *Resolver) ResolveRelated(artifact Artifact, user UserInfo) []string {
	related := make([]string, 0, len(artifact.Related))
	for _, pattern := range artifact.Related {
		expanded := r.expandPath(pattern, user)
		related = append(related, expanded)
	}
	return related
}

// GetSchema returns the appropriate schema for the current OS version.
// Returns nil if no schema matches.
func (r *Resolver) GetSchema(artifact Artifact) *SchemaSpec {
	for i := range artifact.Schema {
		schema := &artifact.Schema[i]
		minVer := schema.OSRange[0]
		maxVer := schema.OSRange[1]

		if minVer != "" && CompareVersions(r.osVersion, minVer) < 0 {
			continue
		}
		if maxVer != "" && CompareVersions(r.osVersion, maxVer) > 0 {
			continue
		}

		return schema
	}
	return nil
}

// OSVersion returns the configured OS version.
func (r *Resolver) OSVersion() string {
	return r.osVersion
}

// AppVersion returns the configured application version.
func (r *Resolver) AppVersion() string {
	return r.appVersion
}

// RootPath returns the configured root path for disk image analysis.
func (r *Resolver) RootPath() string {
	return r.rootPath
}
