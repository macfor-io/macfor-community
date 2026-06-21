// Package schemas registers all community plugin record types in the global
// schema registry. Import this package via _ in the CLI entry point.
package schemas

import (
	"reflect"

	macforparquet "github.com/macfor-io/macfor-community/pkg/macfor/parquet"
	"github.com/macfor-io/macfor-community/plugins/safari"
	"github.com/macfor-io/macfor-community/plugins/shell"
)

//nolint:gochecknoinits // Schema registration requires init.
func init() {
	r := macforparquet.GlobalRegistry()

	// Shell history schemas.
	register(r, "shell.history", "bash_history", reflect.TypeOf(shell.BashRecord{}))
	register(r, "shell.history", "zsh_history", reflect.TypeOf(shell.ZshRecord{}))
	register(r, "shell.history", "fish_history", reflect.TypeOf(shell.FishRecord{}))
	register(r, "shell.history", "sh_history", reflect.TypeOf(shell.ShRecord{}))

	// Safari browser schemas.
	register(r, "browser.safari", "history", reflect.TypeOf(safari.HistoryVisit{}))
	register(r, "browser.safari", "downloads", reflect.TypeOf(safari.DownloadRecord{}))
	register(r, "browser.safari", "bookmarks", reflect.TypeOf(safari.BookmarkRecord{}))
	register(r, "browser.safari", "reading_list", reflect.TypeOf(safari.ReadingListRecord{}))
	register(r, "browser.safari", "cookies", reflect.TypeOf(safari.CookieRecord{}))
	register(r, "browser.safari", "extensions", reflect.TypeOf(safari.ExtensionRecord{}))
	register(r, "browser.safari", "local_storage", reflect.TypeOf(safari.LocalStorageRecord{}))
	register(r, "browser.safari", "session_windows", reflect.TypeOf(safari.SessionWindowRecord{}))
	register(r, "browser.safari", "session_tabs", reflect.TypeOf(safari.SessionTabRecord{}))
}

// register is a helper that computes the checksum and registers a v1.0 schema entry.
func register(r *macforparquet.SchemaRegistry, pluginID, artifactType string, goType reflect.Type) {
	r.Register(macforparquet.SchemaEntry{
		PluginID:     pluginID,
		ArtifactType: artifactType,
		Version:      macforparquet.SchemaVersion{Major: 1, Minor: 0},
		GoType:       goType,
		Checksum:     macforparquet.ComputeSchemaChecksum(goType),
	})
}
