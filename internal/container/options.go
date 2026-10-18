package container

import (
	"regexp"
	"sort"
	"time"
)

// MaskedOptionValue replaces a sensitive plugin option value everywhere the
// option is recorded or displayed. It shows the option was set without
// revealing its value.
const MaskedOptionValue = "[set]"

// CustodyActionPluginOptions records the plugin options configured for a
// collection, with sensitive values masked.
const CustodyActionPluginOptions = "plugin_options"

// secretOptionKey matches option keys whose values are treated as secrets.
var secretOptionKey = regexp.MustCompile(`(?i)(key|token|secret|password|passwd|credential|auth)`)

// IsSecretOptionKey reports whether an option key looks like it names a
// secret (a key, token, password, credential, ...). Such values are always
// masked when recorded, however they were supplied.
func IsSecretOptionKey(key string) bool {
	return secretOptionKey.MatchString(key)
}

// PluginOption is one configured plugin option value.
type PluginOption struct {
	// Value is the option value handed to the plugin.
	Value string

	// Sensitive marks a value that must never be recorded or displayed, for
	// example one read from an environment variable or a file. Values of
	// secret-looking keys are masked regardless of this flag.
	Sensitive bool
}

// masked reports whether the option's value is masked when recorded.
func (o PluginOption) masked(key string) bool {
	return o.Sensitive || IsSecretOptionKey(key)
}

// PluginOptions maps plugin ID to option key to value.
type PluginOptions map[string]map[string]PluginOption

// Redacted returns the options as {plugin_id: {key: value}} with every
// sensitive value replaced by MaskedOptionValue. Non-sensitive literal values
// are kept verbatim so the collection can be reproduced. It returns nil when
// there are no options.
func (p PluginOptions) Redacted() map[string]map[string]string {
	if len(p) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(p))
	for pluginID, opts := range p {
		m := make(map[string]string, len(opts))
		for key, opt := range opts {
			if opt.masked(key) {
				m[key] = MaskedOptionValue
			} else {
				m[key] = opt.Value
			}
		}
		out[pluginID] = m
	}
	return out
}

// PluginIDs returns the plugin IDs that have options, sorted.
func (p PluginOptions) PluginIDs() []string {
	ids := make([]string, 0, len(p))
	for id := range p {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SetPluginOptions records the plugin options configured for this collection.
// Collectors created afterwards with NewCollector expose the options of their
// own plugin through Option. The options are written to the manifest as
// plugin_options and logged as a chain-of-custody event, both with sensitive
// values masked. Call after SetCollectionMetadata. Empty options are ignored.
func (c *Container) SetPluginOptions(opts PluginOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(opts) == 0 {
		return
	}

	// Copy so later changes by the caller do not reach the collectors.
	stored := make(PluginOptions, len(opts))
	for id, m := range opts {
		cp := make(map[string]PluginOption, len(m))
		for k, v := range m {
			cp[k] = v
		}
		stored[id] = cp
	}
	c.pluginOptions = stored

	redacted := stored.Redacted()
	if c.manifest != nil {
		c.manifest.PluginOptions = redacted
	}

	c.addCustodyEntryLocked(ChainOfCustodyEntry{
		Timestamp: time.Now(),
		Action:    CustodyActionPluginOptions,
		Actor:     actorMacfor,
		Details: map[string]any{
			"options": redacted,
		},
	})
}

// pluginOption returns the value of one option configured for pluginID.
func (c *Container) pluginOption(pluginID, name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pluginOptions[pluginID][name].Value
}
