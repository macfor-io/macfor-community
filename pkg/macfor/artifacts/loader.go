package artifacts

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse parses a YAML manifest from bytes.
// The manifest is validated after parsing.
func Parse(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	if err := Validate(&manifest); err != nil {
		return nil, fmt.Errorf("validating manifest: %w", err)
	}

	// Apply defaults.
	applyDefaults(&manifest)
	manifest.source = bytes.Clone(data)

	return &manifest, nil
}

// MustParse parses a manifest and panics on error.
// Useful for embedded manifests in init().
func MustParse(data []byte) *Manifest {
	m, err := Parse(data)
	if err != nil {
		panic(err)
	}
	return m
}

// applyDefaults sets default values for optional fields.
func applyDefaults(m *Manifest) {
	for i := range m.Artifacts {
		for j := range m.Artifacts[i].Paths {
			if m.Artifacts[i].Paths[j].Type == "" {
				m.Artifacts[i].Paths[j].Type = PathTypeFile
			}
		}
	}
}

// ParseWithoutValidation parses a YAML manifest without validation.
// This is useful for debugging or lenient parsing.
func ParseWithoutValidation(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	applyDefaults(&manifest)
	manifest.source = bytes.Clone(data)

	return &manifest, nil
}

// Marshal converts a manifest to YAML bytes.
func Marshal(m *Manifest) ([]byte, error) {
	return yaml.Marshal(m)
}

// GetArtifactByID returns an artifact by ID from a manifest.
// Returns nil if not found.
func GetArtifactByID(m *Manifest, id string) *Artifact {
	for i := range m.Artifacts {
		if m.Artifacts[i].ID == id {
			return &m.Artifacts[i]
		}
	}
	return nil
}

// GetArtifactsByCategory returns all artifacts matching a category.
func GetArtifactsByCategory(m *Manifest, category string) []Artifact {
	var result []Artifact
	for _, a := range m.Artifacts {
		if a.Category == category {
			result = append(result, a)
		}
	}
	return result
}

// GetArtifactsByFormat returns all artifacts matching a format.
func GetArtifactsByFormat(m *Manifest, format string) []Artifact {
	var result []Artifact
	for _, a := range m.Artifacts {
		if a.Format == format {
			result = append(result, a)
		}
	}
	return result
}

// GetOptionalArtifacts returns all optional artifacts.
func GetOptionalArtifacts(m *Manifest) []Artifact {
	var result []Artifact
	for _, a := range m.Artifacts {
		if a.Optional {
			result = append(result, a)
		}
	}
	return result
}

// GetRequiredArtifacts returns all required (non-optional) artifacts.
func GetRequiredArtifacts(m *Manifest) []Artifact {
	var result []Artifact
	for _, a := range m.Artifacts {
		if !a.Optional {
			result = append(result, a)
		}
	}
	return result
}

// GetRootRequiredArtifacts returns all artifacts requiring root privileges.
func GetRootRequiredArtifacts(m *Manifest) []Artifact {
	var result []Artifact
	for _, a := range m.Artifacts {
		if a.RequiresRoot {
			result = append(result, a)
		}
	}
	return result
}
