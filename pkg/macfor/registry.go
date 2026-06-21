package macfor

import "sync"

// registry stores all registered plugins indexed by their ID.
var (
	registry   = make(map[string]Plugin)
	registryMu sync.RWMutex
)

// Register adds a plugin to the global registry.
// Plugins should call this from their init() function.
// If a plugin with the same ID is already registered, it will be overwritten.
func Register(p Plugin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.Info().ID] = p
}

// All returns all registered plugins as a slice.
// The order of plugins is not guaranteed.
func All() []Plugin {
	registryMu.RLock()
	defer registryMu.RUnlock()

	plugins := make([]Plugin, 0, len(registry))
	for _, p := range registry {
		plugins = append(plugins, p)
	}
	return plugins
}

// ByID returns the plugin with the given ID.
// Returns nil, false if no plugin with that ID is registered.
func ByID(id string) (Plugin, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	p, ok := registry[id]
	return p, ok
}

// ByCategory returns all plugins that have the given category.
// Returns an empty slice if no plugins match.
func ByCategory(category string) []Plugin {
	registryMu.RLock()
	defer registryMu.RUnlock()

	var plugins []Plugin
	for _, p := range registry {
		for _, cat := range p.Info().Categories {
			if cat == category {
				plugins = append(plugins, p)
				break
			}
		}
	}
	return plugins
}

// ClearRegistry removes all plugins from the registry.
// This is intended for use in tests only.
func ClearRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Plugin)
}

// RegisteredCount returns the number of registered plugins.
// This is intended for use in tests only.
func RegisteredCount() int {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return len(registry)
}
