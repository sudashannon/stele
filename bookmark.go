package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"stele/internal/appdir"
)

// Bookmark is a starred document, persisted as a flat JSON array to
// the panel data directory (see internal/appdir).
type Bookmark struct {
	Path      string `json:"path"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	StarredAt string `json:"starredAt"`
}

var (
	bookmarksMu sync.Mutex
)

// bookmarksPath returns the on-disk location of the bookmarks file.
func bookmarksPath() string {
	return appdir.Path("bookmarks.json")
}

// loadBookmarks reads the bookmarks file. A missing file is not an error —
// it means nothing has been starred yet.
func loadBookmarks(path string) ([]Bookmark, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Bookmark{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Bookmark
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Bookmark{}
	}
	return out, nil
}

// persistBookmarks writes the bookmarks list, creating the parent directory
// if needed.
func persistBookmarks(path string, bookmarks []Bookmark) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(bookmarks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// handleBookmarks serves GET/POST/DELETE /api/bookmarks against the file at
// bookmarksPath(). GET lists all bookmarks; POST adds one (deduping by
// path); DELETE removes one by path (from the JSON body or ?path= query).
func handleBookmarks(w http.ResponseWriter, r *http.Request) {
	handleBookmarksAt(w, r, bookmarksPath())
}

// handleBookmarksAt is handleBookmarks with an injectable file path, so
// tests can point it at a temp file instead of the real HOME.
func handleBookmarksAt(w http.ResponseWriter, r *http.Request, path string) {
	bookmarksMu.Lock()
	defer bookmarksMu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		bookmarks, err := loadBookmarks(path)
		if err != nil {
			writeJSONError(w, "failed to read bookmarks", 500)
			return
		}
		json.NewEncoder(w).Encode(bookmarks)

	case http.MethodPost:
		var b Bookmark
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Path == "" {
			writeJSONError(w, "invalid body", 400)
			return
		}
		bookmarks, err := loadBookmarks(path)
		if err != nil {
			writeJSONError(w, "failed to read bookmarks", 500)
			return
		}
		if b.StarredAt == "" {
			b.StarredAt = time.Now().UTC().Format(time.RFC3339)
		}
		replaced := false
		for i, existing := range bookmarks {
			if existing.Path == b.Path {
				bookmarks[i] = b
				replaced = true
				break
			}
		}
		if !replaced {
			bookmarks = append(bookmarks, b)
		}
		if err := persistBookmarks(path, bookmarks); err != nil {
			writeJSONError(w, "failed to save bookmarks", 500)
			return
		}
		json.NewEncoder(w).Encode(bookmarks)

	case http.MethodDelete:
		target := r.URL.Query().Get("path")
		if target == "" {
			var body struct {
				Path string `json:"path"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			target = body.Path
		}
		if target == "" {
			writeJSONError(w, "path required", 400)
			return
		}
		bookmarks, err := loadBookmarks(path)
		if err != nil {
			writeJSONError(w, "failed to read bookmarks", 500)
			return
		}
		out := make([]Bookmark, 0, len(bookmarks))
		for _, b := range bookmarks {
			if b.Path != target {
				out = append(out, b)
			}
		}
		if err := persistBookmarks(path, out); err != nil {
			writeJSONError(w, "failed to save bookmarks", 500)
			return
		}
		json.NewEncoder(w).Encode(out)

	default:
		writeJSONError(w, "method not allowed", 405)
	}
}
