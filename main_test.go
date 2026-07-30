package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServesEmbeddedIndex(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	staticHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// A tab left open across a redeploy kept serving the previously fetched
// index.html — and with it the old asset hashes — because the embedded FS
// exposes no ModTime, so http.FileServer sent no validator at all. The entry
// document must always revalidate; the fingerprinted assets never need to.
func TestStaticCacheHeaders(t *testing.T) {
	for _, tc := range []struct {
		path string
		want string
	}{
		{"/", "no-cache"},
		{"/index.html", "no-cache"},
		{"/assets/", "public, max-age=31536000, immutable"},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		staticHandler().ServeHTTP(w, req)
		if got := w.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("%s: Cache-Control = %q, want %q", tc.path, got, tc.want)
		}
	}
}
