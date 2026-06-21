package safari

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"howett.net/plist"
)

// BookmarkRecord represents a Safari bookmark from Bookmarks.plist.
// Each record corresponds to a single bookmark (leaf node) in the bookmark tree.
type BookmarkRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who owns this bookmark.
	User string `json:"user"`

	// URL is the bookmarked page URL.
	URL string `json:"url"`

	// Title is the bookmark's display title.
	Title string `json:"title"`

	// FolderPath is the hierarchical path to this bookmark.
	// Example: "BookmarksBar/Work/Research".
	FolderPath string `json:"folder_path"`

	// UUID is the unique identifier assigned by Safari.
	UUID string `json:"uuid"`

	// DateAdded is when the bookmark was created (if available).
	DateAdded time.Time `json:"date_added,omitempty"`

	// SourceFile is the path to the Bookmarks.plist file on the source system.
	SourceFile string `json:"source_file"`
}

// ReadingListRecord represents a Safari Reading List item from Bookmarks.plist.
// Reading List items are a special type of bookmark with additional metadata.
type ReadingListRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who owns this reading list item.
	User string `json:"user"`

	// URL is the saved page URL.
	URL string `json:"url"`

	// Title is the article/page title.
	Title string `json:"title"`

	// PreviewText is a snippet of the article content.
	PreviewText string `json:"preview_text,omitempty"`

	// DateAdded is when the item was added to the reading list.
	DateAdded time.Time `json:"date_added"`

	// DateLastFetched is when the article was last fetched/synced.
	DateLastFetched time.Time `json:"date_last_fetched,omitempty"`

	// UUID is the unique identifier assigned by Safari.
	UUID string `json:"uuid"`

	// SourceFile is the path to the Bookmarks.plist file on the source system.
	SourceFile string `json:"source_file"`
}

// bookmarkNode represents a node in the Safari bookmarks tree structure.
// This can be either a folder (WebBookmarkTypeList) or a leaf bookmark (WebBookmarkTypeLeaf).
type bookmarkNode struct {
	WebBookmarkType string            `plist:"WebBookmarkType"`
	WebBookmarkUUID string            `plist:"WebBookmarkUUID"`
	Title           string            `plist:"Title"`
	URLString       string            `plist:"URLString"`
	URIDictionary   map[string]string `plist:"URIDictionary"`
	Children        []bookmarkNode    `plist:"Children"`
	ReadingList     *readingListData  `plist:"ReadingList"`
}

// readingListData contains additional metadata for Reading List items.
type readingListData struct {
	DateAdded       time.Time `plist:"DateAdded"`
	DateLastFetched time.Time `plist:"DateLastFetched"`
	PreviewText     string    `plist:"PreviewText"`
}

// Bookmark type constants from Safari's plist format.
const (
	// bookmarkTypeLeaf indicates a bookmark (leaf node).
	bookmarkTypeLeaf = "WebBookmarkTypeLeaf"

	// bookmarkTypeList indicates a folder (container node).
	bookmarkTypeList = "WebBookmarkTypeList"

	// readingListFolderTitle is the special folder name for Reading List.
	readingListFolderTitle = "com.apple.ReadingList"

	// bookmarksBarTitle is the special folder name for Bookmarks Bar.
	bookmarksBarTitle = "BookmarksBar"

	// bookmarksMenuTitle is the special folder name for Bookmarks Menu.
	bookmarksMenuTitle = "BookmarksMenu"
)

// collectBookmarksForUser collects Safari bookmarks from Bookmarks.plist for a single user.
// It reads the plist file, recursively parses the bookmark tree, and adds records
// to the collector along with the raw plist file. Reading List items are extracted
// separately with their additional metadata.
func collectBookmarksForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	bookmarksPath, err := locator.Path(src, user, string(ArtifactBookmarks))
	if err != nil {
		return err
	}
	if bookmarksPath == "" {
		out.Log(macfor.LogDebug, "Bookmarks.plist not found for user %s", user.Username)
		return nil
	}

	// Check if the bookmarks file exists.
	fileInfo, err := src.Stat(bookmarksPath)
	if err != nil {
		if errors.Is(err, macfor.ErrNotFound) || errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			out.Log(macfor.LogDebug, "Bookmarks.plist not found for user %s", user.Username)
			return nil
		}
		return fmt.Errorf("checking Bookmarks.plist: %w", err)
	}

	// Create artifact for the raw file and parsed records.
	artifact, err := macfor.NewArtifact(PluginID, "browser", "bookmarks")
	if err != nil {
		return fmt.Errorf("creating artifact: %w", err)
	}
	artifact.SourcePath = bookmarksPath
	artifact.Username = user.Username
	artifact.Description = "Safari bookmarks and reading list"
	artifact.OriginalModTime = fileInfo.ModTime()
	artifact.OriginalSize = fileInfo.Size()

	// Read the plist file.
	rawFile, err := src.Open(bookmarksPath)
	if err != nil {
		return fmt.Errorf("opening Bookmarks.plist: %w", err)
	}

	// Read all content for parsing and container storage.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rawFile); err != nil {
		_ = rawFile.Close()
		return fmt.Errorf("reading Bookmarks.plist: %w", err)
	}
	_ = rawFile.Close()
	plistData := buf.Bytes()

	// Add raw file to container from the already-read buffer.
	if err := out.AddFile(ctx, *artifact, bytes.NewReader(plistData)); err != nil {
		return fmt.Errorf("adding Bookmarks.plist file: %w", err)
	}

	// Parse the plist.
	var root bookmarkNode
	decoder := plist.NewDecoder(bytes.NewReader(plistData))
	if err := decoder.Decode(&root); err != nil {
		out.Log(macfor.LogWarn, "Error parsing Bookmarks.plist for user %s: %v", user.Username, err)
		return fmt.Errorf("parsing Bookmarks.plist: %w", err)
	}

	// Walk the bookmark tree and emit records.
	bookmarkCount := 0
	readingListCount := 0

	err = walkBookmarks(ctx, root, "", user.Username, bookmarksPath, artifact, out, &bookmarkCount, &readingListCount)
	if err != nil {
		return err
	}

	if bookmarkCount == 0 && readingListCount == 0 {
		out.Log(macfor.LogInfo, "No bookmarks found for user %s", user.Username)
	} else {
		out.Log(macfor.LogInfo, "Collected %d bookmarks and %d reading list items for user %s",
			bookmarkCount, readingListCount, user.Username)
	}

	return nil
}

// walkBookmarks recursively walks the bookmark tree and emits records for each bookmark.
// It tracks the current folder path to provide context for each bookmark.
func walkBookmarks(
	ctx context.Context,
	node bookmarkNode,
	currentPath string,
	username string,
	sourcePath string,
	artifact *macfor.Artifact,
	out macfor.Collector,
	bookmarkCount *int,
	readingListCount *int,
) error {
	// Check for context cancellation.
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Determine the node's contribution to the path.
	nodePath := currentPath
	if node.Title != "" && node.Title != readingListFolderTitle {
		if nodePath == "" {
			nodePath = node.Title
		} else {
			nodePath = nodePath + "/" + node.Title
		}
	}

	switch node.WebBookmarkType {
	case bookmarkTypeLeaf:
		// This is a leaf bookmark - emit a record.
		// Check if this is a reading list item.
		if node.ReadingList != nil {
			record := convertReadingListNode(node, username, sourcePath)
			if err := out.AddRecord(ctx, *artifact, record); err != nil {
				out.Log(macfor.LogWarn, "Error adding reading list record: %v", err)
			} else {
				*readingListCount++
			}
		} else {
			record := convertBookmarkNode(node, currentPath, username, sourcePath)
			if err := out.AddRecord(ctx, *artifact, record); err != nil {
				out.Log(macfor.LogWarn, "Error adding bookmark record: %v", err)
			} else {
				*bookmarkCount++
			}
		}

	case bookmarkTypeList:
		// This is a folder - recurse into children.
		// Special handling for Reading List folder.
		isReadingList := node.Title == readingListFolderTitle

		for _, child := range node.Children {
			childPath := nodePath
			if isReadingList {
				// For reading list items, don't include the folder path.
				childPath = ""
			}

			if err := walkBookmarks(ctx, child, childPath, username, sourcePath, artifact, out, bookmarkCount, readingListCount); err != nil {
				return err
			}
		}
	}

	return nil
}

// convertBookmarkNode converts a bookmark node to a BookmarkRecord.
func convertBookmarkNode(node bookmarkNode, folderPath, username, sourcePath string) *BookmarkRecord {
	// Get title from URIDictionary if available, otherwise use node Title.
	title := node.Title
	if t, ok := node.URIDictionary["title"]; ok && t != "" {
		title = t
	}

	return &BookmarkRecord{
		Type:       "browser_bookmark",
		Browser:    browserSafari,
		User:       username,
		URL:        node.URLString,
		Title:      title,
		FolderPath: folderPath,
		UUID:       node.WebBookmarkUUID,
		SourceFile: sourcePath,
	}
}

// convertReadingListNode converts a reading list node to a ReadingListRecord.
func convertReadingListNode(node bookmarkNode, username, sourcePath string) *ReadingListRecord {
	// Get title from URIDictionary if available, otherwise use node Title.
	title := node.Title
	if t, ok := node.URIDictionary["title"]; ok && t != "" {
		title = t
	}

	record := &ReadingListRecord{
		Type:       "reading_list_item",
		Browser:    browserSafari,
		User:       username,
		URL:        node.URLString,
		Title:      title,
		UUID:       node.WebBookmarkUUID,
		SourceFile: sourcePath,
	}

	// Add reading list specific metadata.
	if node.ReadingList != nil {
		record.DateAdded = node.ReadingList.DateAdded
		record.DateLastFetched = node.ReadingList.DateLastFetched
		record.PreviewText = node.ReadingList.PreviewText
	}

	return record
}
