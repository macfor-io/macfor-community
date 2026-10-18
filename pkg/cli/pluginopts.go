package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Plugin option value-source prefixes.
const (
	// optSourceEnv reads the value from an environment variable.
	optSourceEnv = "env:"

	// optSourceFile reads the value from a file, trimming one trailing newline.
	optSourceFile = "file:"

	// optSourceLiteral marks a literal value, so a value that itself begins
	// with "env:" or "file:" can be passed verbatim.
	optSourceLiteral = "literal:"
)

// pluginOptFlag is the name of the plugin option flag.
const pluginOptFlag = "--plugin-opt"

// errMalformedPluginOpt is wrapped by every syntax error in a --plugin-opt entry.
var errMalformedPluginOpt = errors.New("malformed " + pluginOptFlag)

// pluginOptEnv abstracts the environment and filesystem lookups used to
// resolve env: and file: option values, so tests can replace them.
type pluginOptEnv struct {
	lookupEnv func(string) (string, bool)
	readFile  func(string) ([]byte, error)
}

// defaultPluginOptEnv resolves values from the process environment and the
// local filesystem.
var defaultPluginOptEnv = pluginOptEnv{
	lookupEnv: os.LookupEnv,
	readFile:  os.ReadFile,
}

// registeredPluginIDs returns the IDs of every registered plugin.
func registeredPluginIDs() []string {
	plugins := macfor.All()
	ids := make([]string, 0, len(plugins))
	for _, p := range plugins {
		ids = append(ids, p.Info().ID)
	}
	return ids
}

// parsePluginOpts parses repeated --plugin-opt entries of the form
// <plugin_id>.<key>=<value> or <plugin_id>:<key>=<value>. Plugin IDs contain
// dots, so the plugin is the longest registered ID that prefixes the entry and
// is followed by '.' or ':'. The value is a literal, env:NAME (an environment
// variable, which must be set), file:PATH (a file's contents with one trailing
// newline removed), or literal:VALUE (VALUE verbatim). Values from env: and
// file: are marked sensitive.
//
// Errors never include an option value: they quote only the part of the entry
// before the first '='.
func parsePluginOpts(entries, pluginIDs []string, env pluginOptEnv) (container.PluginOptions, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Longest IDs first, so the first match is the longest prefix.
	ids := append([]string(nil), pluginIDs...)
	sort.Slice(ids, func(i, j int) bool { return len(ids[i]) > len(ids[j]) })

	opts := make(container.PluginOptions)
	for i, entry := range entries {
		spec, rawValue, hasEq := strings.Cut(entry, "=")
		if !hasEq {
			// The entry is not quoted: without '=' a value cannot be told
			// apart from the key, and it may be a secret.
			return nil, fmt.Errorf("%w (entry %d): expected <plugin_id>.<key>=<value>", errMalformedPluginOpt, i+1)
		}

		pluginID, key, err := splitPluginOptSpec(spec, ids)
		if err != nil {
			return nil, err
		}

		opt, err := resolvePluginOptValue(rawValue, env)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", pluginOptFlag, spec, err)
		}

		if opts[pluginID] == nil {
			opts[pluginID] = make(map[string]container.PluginOption)
		}
		if _, dup := opts[pluginID][key]; dup {
			return nil, fmt.Errorf("%s %q: option set more than once", pluginOptFlag, spec)
		}
		opts[pluginID][key] = opt
	}
	return opts, nil
}

// splitPluginOptSpec splits "<plugin_id>.<key>" or "<plugin_id>:<key>" into
// its plugin ID and key, matching the longest registered plugin ID. ids must
// be sorted longest first.
func splitPluginOptSpec(spec string, ids []string) (pluginID, key string, err error) {
	for _, id := range ids {
		if spec == id {
			return "", "", fmt.Errorf("%w %q: missing option key after plugin %s", errMalformedPluginOpt, spec, id)
		}
		if len(spec) <= len(id) || !strings.HasPrefix(spec, id) {
			continue
		}
		if sep := spec[len(id)]; sep != '.' && sep != ':' {
			continue
		}
		pluginID, key = id, spec[len(id)+1:]
		break
	}

	if pluginID == "" {
		return "", "", fmt.Errorf("%s %q: unknown plugin (run 'macfor list plugins' for valid IDs)", pluginOptFlag, spec)
	}
	if key == "" {
		return "", "", fmt.Errorf("%w %q: missing option key after plugin %s", errMalformedPluginOpt, spec, pluginID)
	}
	if strings.IndexFunc(key, unicode.IsSpace) >= 0 {
		return "", "", fmt.Errorf("%w %q: option key contains whitespace", errMalformedPluginOpt, spec)
	}
	return pluginID, key, nil
}

// resolvePluginOptValue resolves a raw --plugin-opt value according to its
// source prefix.
func resolvePluginOptValue(raw string, env pluginOptEnv) (container.PluginOption, error) {
	switch {
	case strings.HasPrefix(raw, optSourceEnv):
		name := strings.TrimPrefix(raw, optSourceEnv)
		if name == "" {
			return container.PluginOption{}, fmt.Errorf("%w: env: needs a variable name", errMalformedPluginOpt)
		}
		v, ok := env.lookupEnv(name)
		if !ok {
			return container.PluginOption{}, fmt.Errorf("environment variable %s is not set", name)
		}
		return container.PluginOption{Value: v, Sensitive: true}, nil

	case strings.HasPrefix(raw, optSourceFile):
		path := strings.TrimPrefix(raw, optSourceFile)
		if path == "" {
			return container.PluginOption{}, fmt.Errorf("%w: file: needs a path", errMalformedPluginOpt)
		}
		data, err := env.readFile(path)
		if err != nil {
			// The error names only the path, never the file's contents.
			return container.PluginOption{}, fmt.Errorf("reading option value file: %w", err)
		}
		v := string(data)
		if strings.HasSuffix(v, "\r\n") {
			v = strings.TrimSuffix(v, "\r\n")
		} else {
			v = strings.TrimSuffix(v, "\n")
		}
		return container.PluginOption{Value: v, Sensitive: true}, nil

	case strings.HasPrefix(raw, optSourceLiteral):
		return container.PluginOption{Value: strings.TrimPrefix(raw, optSourceLiteral)}, nil

	default:
		return container.PluginOption{Value: raw}, nil
	}
}

// warnLiteralSecretOptions warns, without the value, about secret-looking
// options passed as literals, which leak into shell history and ps output.
func warnLiteralSecretOptions(opts container.PluginOptions) {
	for _, id := range opts.PluginIDs() {
		for key, opt := range opts[id] {
			if !opt.Sensitive && container.IsSecretOptionKey(key) {
				logger.Warn().Str("plugin", id).Str("option", key).
					Msg("Secret-looking plugin option passed as a literal; prefer env:NAME or file:PATH to keep it out of shell history and ps")
			}
		}
	}
}

// printPluginOptions writes the configured plugin options, sensitive values
// masked, one per line. It writes nothing when there are none.
func printPluginOptions(w io.Writer, redacted map[string]map[string]string) {
	if len(redacted) == 0 {
		return
	}
	ids := make([]string, 0, len(redacted))
	for id := range redacted {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	_, _ = fmt.Fprintln(w, "Plugin options:")
	for _, id := range ids {
		keys := make([]string, 0, len(redacted[id]))
		for k := range redacted[id] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = fmt.Fprintf(w, "  %s.%s = %s\n", id, k, redacted[id][k])
		}
	}
	_, _ = fmt.Fprintln(w)
}
