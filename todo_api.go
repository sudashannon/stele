package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"comet-ui/internal/todo"
	"comet-ui/wiki"
)

// todoAPI bundles the shared Todo Store and a reference to the wiki API.
type todoAPI struct {
	store   *todo.Store
	wikiAPI *wiki.API
}

func newTodoAPI(store *todo.Store, wikiAPI *wiki.API) *todoAPI {
	return &todoAPI{store: store, wikiAPI: wikiAPI}
}

// writeGuard checks whether the request is a local mutation.
func writeGuard(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if !net.ParseIP(host).IsLoopback() {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		reqHost := r.Host
		if !todo.SameOrigin(origin, reqHost) {
			return false
		}
	}
	return true
}

// resolveWikiTitles resolves each WikiRef's current title and workspace from
// the live wiki graph, falling back to the snapshot when the component is
// not found. Does not hold graph locks during JSON writes.
func (a *todoAPI) resolveWikiTitles(refs []todo.WikiRef) []todo.WikiRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]todo.WikiRef, len(refs))
	copy(out, refs)
	for i := range out {
		c, ok := a.wikiAPI.ComponentByID(out[i].ComponentID)
		if ok {
			if c.Title != "" {
				out[i].TitleSnapshot = c.Title
			}
			if c.Workspace != "" {
				out[i].Workspace = c.Workspace
			}
		}
	}
	return out
}

func (a *todoAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/todos")
	if path == "" || path == "/" {
		switch r.Method {
		case http.MethodGet:
			a.handleList(w, r)
		case http.MethodPost:
			if !writeGuard(r) {
				writeJSONError(w, "write access denied", http.StatusForbidden)
				return
			}
			a.handleCreate(w, r)
		default:
			writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	id := strings.TrimPrefix(path, "/")
	if id == "" {
		writeJSONError(w, "missing id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		if !writeGuard(r) {
			writeJSONError(w, "write access denied", http.StatusForbidden)
			return
		}
		a.handleUpdate(w, r, id)
	case http.MethodDelete:
		if !writeGuard(r) {
			writeJSONError(w, "write access denied", http.StatusForbidden)
			return
		}
		a.handleDelete(w, r, id)
	case http.MethodGet, http.MethodPost:
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
	default:
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *todoAPI) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := todo.Filter{
		Status:          todo.Status(q.Get("status")),
		Workspace:       q.Get("workspace"),
		Change:          q.Get("change"),
		WikiComponentID: q.Get("wikiComponentId"),
		Q:               q.Get("q"),
	}

	items, counts, revision := a.store.List(f)

	for i := range items {
		items[i].WikiRefs = a.resolveWikiTitles(items[i].WikiRefs)
	}

	writable := writeGuard(r)

	resp := map[string]any{
		"items":    items,
		"counts":   counts,
		"revision": revision,
		"writable": writable,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (a *todoAPI) handleCreate(w http.ResponseWriter, r *http.Request) {
	var in todo.CreateInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSONError(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if dec.More() {
		writeJSONError(w, "trailing data", http.StatusBadRequest)
		return
	}

	// REST defaults metadata.source to "ui".
	if in.Metadata.Source == "" {
		in.Metadata.Source = todo.SourceUI
	}

	item, err := a.store.Create(in)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	item.WikiRefs = a.resolveWikiTitles(item.WikiRefs)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

func (a *todoAPI) handleUpdate(w http.ResponseWriter, r *http.Request, id string) {
	var in todo.UpdateInput
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSONError(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if dec.More() {
		writeJSONError(w, "trailing data", http.StatusBadRequest)
		return
	}

	item, err := a.store.Update(id, in)
	if err != nil {
		if err == todo.ErrNotFound {
			writeJSONError(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	item.WikiRefs = a.resolveWikiTitles(item.WikiRefs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(item)
}

func (a *todoAPI) handleDelete(w http.ResponseWriter, r *http.Request, id string) {
	if err := a.store.Delete(id); err != nil {
		if err == todo.ErrNotFound {
			writeJSONError(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}
