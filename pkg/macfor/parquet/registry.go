package parquet

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// SchemaVersion represents a major.minor version for a schema.
type SchemaVersion struct {
	Major int
	Minor int
}

// String returns the version in "major.minor" format.
func (v SchemaVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// SchemaEntry describes the schema for a specific (plugin_id, artifact_type) pair.
type SchemaEntry struct {
	PluginID     string
	ArtifactType string
	Version      SchemaVersion
	Checksum     string
	GoType       reflect.Type
}

// registryKey builds the lookup key for a schema entry.
func registryKey(pluginID, artifactType string) string {
	return pluginID + ":" + artifactType
}

// SchemaRegistry maps (plugin_id, artifact_type) pairs to schema entries.
// It is safe for concurrent use.
type SchemaRegistry struct {
	mu      sync.RWMutex
	entries map[string]*SchemaEntry
}

// NewSchemaRegistry creates a new empty schema registry.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		entries: make(map[string]*SchemaEntry),
	}
}

// globalRegistry is the singleton registry instance.
var globalRegistry = NewSchemaRegistry()

// GlobalRegistry returns the process-wide schema registry.
func GlobalRegistry() *SchemaRegistry {
	return globalRegistry
}

// Register stores a schema entry. If an entry already exists for the same
// (plugin_id, artifact_type), it is overwritten.
func (r *SchemaRegistry) Register(entry SchemaEntry) {
	key := registryKey(entry.PluginID, entry.ArtifactType)
	r.mu.Lock()
	r.entries[key] = &entry
	r.mu.Unlock()
}

// Lookup returns the schema entry for the given (plugin_id, artifact_type),
// or nil if not found.
func (r *SchemaRegistry) Lookup(pluginID, artifactType string) *SchemaEntry {
	key := registryKey(pluginID, artifactType)
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[key]
}

// Entries returns all registered schema entries, sorted by key.
func (r *SchemaRegistry) Entries() []*SchemaEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]*SchemaEntry, 0, len(keys))
	for _, k := range keys {
		result = append(result, r.entries[k])
	}
	return result
}

// Len returns the number of registered schema entries.
func (r *SchemaRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
