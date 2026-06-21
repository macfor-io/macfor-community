// Package collect provides manifest-driven helpers for locating artifacts on a
// forensic source. It lets plugins resolve artifact paths from their embedded
// artifacts.yaml manifest — honouring OS/app version constraints and path types
// — instead of hardcoding paths as Go constants.
//
// A Locator binds a parsed manifest to the resolution helpers so plugins do not
// repeat the manifest lookup at every call site. The generic logic lives here
// once and is shared across all plugins; only genuinely plugin-specific
// presence logic (e.g. walking a directory tree) stays in the plugin.
package collect

import (
	"errors"
	"fmt"
	"strings"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// AppVersionFunc reports the installed version of the application a plugin
// targets (e.g. read from the app bundle's Info.plist on the source). An empty
// return means the version could not be determined; app-version path
// constraints (app_min/app_max) are then not applied, so unknown degrades to
// "include the path" rather than silently dropping artifacts.
type AppVersionFunc func(src macfor.Source) string

// Option configures a Locator.
type Option func(*Locator)

// WithAppVersion supplies the function used to detect the target application's
// version on a source. Without it, app_min/app_max constraints in the manifest
// are display-only — they appear in the catalog but are never enforced during
// path resolution.
func WithAppVersion(fn AppVersionFunc) Option {
	return func(l *Locator) { l.appVersion = fn }
}

// Locator resolves artifact locations from a plugin manifest.
type Locator struct {
	manifest   *artifacts.Manifest
	appVersion AppVersionFunc
}

// NewLocator returns a Locator bound to the given parsed manifest.
func NewLocator(m *artifacts.Manifest, opts ...Option) *Locator {
	l := &Locator{manifest: m}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// resolverForSource builds a version-aware path resolver from the source's
// reported macOS version and, when an AppVersionFunc is configured, the
// detected application version. The resolver drives manifest path expansion
// and OS/app-version-gated path selection.
func (l *Locator) resolverForSource(src macfor.Source) (*artifacts.Resolver, error) {
	osVersion, err := src.OSVersion()
	if err != nil {
		return nil, fmt.Errorf("reading OS version: %w", err)
	}
	r := artifacts.NewResolver(osVersion)
	if l.appVersion != nil {
		if v := l.appVersion(src); v != "" {
			r = r.WithAppVersion(v)
		}
	}
	return r, nil
}

// Paths returns the manifest-resolved paths for the given artifact ID and user,
// using the source's OS version (and the detected app version, when configured
// via WithAppVersion) for version-aware selection. The artifact ID must match
// an artifact entry in the manifest.
//
// Returns an empty slice (not an error) when no path matches the current OS
// and app versions, so callers can treat a missing artifact as a graceful skip.
func (l *Locator) Paths(src macfor.Source, user macfor.User, artifactID string) ([]artifacts.ResolvedPath, error) {
	def := artifacts.GetArtifactByID(l.manifest, artifactID)
	if def == nil {
		return nil, fmt.Errorf("artifact %q not defined in manifest", artifactID)
	}

	r, err := l.resolverForSource(src)
	if err != nil {
		return nil, err
	}

	paths, err := r.ResolveArtifact(*def, artifacts.SimpleUser{
		Username: user.Username,
		HomeDir:  user.HomeDir,
	})
	if err != nil {
		// A required artifact with no version-matching path is treated as
		// "not present on this OS" rather than a hard failure, matching the
		// graceful-degradation contract of collection plugins.
		if errors.Is(err, artifacts.ErrNoMatchingPath) {
			return nil, nil
		}
		return nil, err
	}

	return paths, nil
}

// Path returns the first manifest-resolved path for an artifact and user. It is
// a convenience wrapper for single-path artifacts. Returns an empty string when
// the artifact resolves to no paths on this OS version.
func (l *Locator) Path(src macfor.Source, user macfor.User, artifactID string) (string, error) {
	paths, err := l.Paths(src, user, artifactID)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0].Path, nil
}

// Present reports whether any manifest-resolved path for the artifact exists on
// the source. Presence is checked according to each path's declared type (file,
// glob, or directory). Errors are treated as "not present" so a permission or
// lookup failure on one artifact never aborts detection.
func (l *Locator) Present(src macfor.Source, user macfor.User, artifactID string) bool {
	paths, err := l.Paths(src, user, artifactID)
	if err != nil {
		return false
	}

	for _, rp := range paths {
		switch rp.Type {
		case artifacts.PathTypeGlob:
			matches, err := src.Glob(rp.Path)
			if err == nil && len(matches) > 0 {
				return true
			}
		case artifacts.PathTypeDirectory:
			info, err := src.Stat(rp.Path)
			if err == nil && info.IsDir() {
				return true
			}
		default: // PathTypeFile.
			if _, err := src.Stat(rp.Path); err == nil {
				return true
			}
		}
	}

	return false
}

// PathPattern returns the i-th raw path pattern declared for an artifact in the
// manifest (without ~ expansion), for plugins that derive package-level path
// constants from the manifest as their single source of truth. It panics if the
// artifact or index is missing — a manifest/code mismatch caught at init.
func PathPattern(m *artifacts.Manifest, artifactID string, i int) string {
	def := artifacts.GetArtifactByID(m, artifactID)
	if def == nil || i < 0 || i >= len(def.Paths) {
		panic(fmt.Sprintf("collect: manifest missing path %d for artifact %q", i, artifactID))
	}
	return def.Paths[i].Pattern
}

// HomeRelPattern returns PathPattern with a leading "~/" stripped, for paths
// that are joined with a user's home directory.
func HomeRelPattern(m *artifacts.Manifest, artifactID string, i int) string {
	return strings.TrimPrefix(PathPattern(m, artifactID, i), "~/")
}

// ProfileValue returns the named profile/location string declared in the
// manifest's profiles map, for plugins that derive package-level profile
// constants from the manifest as their single source of truth. It panics if
// the key is missing — a manifest/code mismatch caught at init.
func ProfileValue(m *artifacts.Manifest, key string) string {
	v, ok := m.Profiles[key]
	if !ok {
		panic(fmt.Sprintf("collect: manifest missing profile key %q", key))
	}
	return v
}

// Variants returns the application variants declared in the manifest, in
// declaration order. It panics if none are declared — a manifest/code mismatch
// caught at init.
func Variants(m *artifacts.Manifest) []artifacts.VariantSpec {
	if len(m.Variants) == 0 {
		panic("collect: manifest declares no variants")
	}
	return m.Variants
}

// PathPatterns returns every path pattern declared for an artifact (no ~
// expansion). It panics if the artifact is missing.
func PathPatterns(m *artifacts.Manifest, artifactID string) []string {
	def := artifacts.GetArtifactByID(m, artifactID)
	if def == nil {
		panic(fmt.Sprintf("collect: manifest missing artifact %q", artifactID))
	}
	out := make([]string, len(def.Paths))
	for i, p := range def.Paths {
		out[i] = p.Pattern
	}
	return out
}
