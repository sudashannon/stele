package wiki

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const communityDirtyThreshold = 5

// Watcher watches a set of workspace directories for changes to markdown
// and .comet.yaml files and triggers incremental wiki index updates.
//
// File-change events are debounced by `debounce` before triggering a
// rebuild, and community detection (more expensive: Louvain over the whole
// graph) is further debounced by `communityDelay` after any structural
// change so a burst of edits during a save-heavy edit session only pays
// for community re-detection once, after things settle.
type Watcher struct {
	api            *API
	scriptPath     string
	debounce       time.Duration
	communityDelay time.Duration
	watcher        *fsnotify.Watcher
	stop           chan struct{}
	wg             sync.WaitGroup
	mirror         *Mirror
}

// NewWatcher constructs a Watcher bound to api. IncrementalUpdate resolves
// the embedding script through the same findEmbedScript path as BuildIndex;
// scriptPath remains available for callers that override watcher wiring.
func NewWatcher(api *API, scriptPath string) *Watcher {
	return &Watcher{
		api:            api,
		scriptPath:     scriptPath,
		debounce:       5 * time.Second,
		communityDelay: 30 * time.Second,
		stop:           make(chan struct{}),
	}
}

// Start begins watching the given directory paths (recursively) for .md and
// .comet.yaml changes. It spawns a background goroutine and returns
// immediately; it never blocks the caller (e.g. the HTTP server).
func (w *Watcher) Start(paths []string) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = fw

	for _, root := range paths {
		w.addWatchTree(root)
	}

	log.Printf("wiki watcher: watching %d paths", len(paths))
	w.wg.Add(1)
	go w.loop()
	return nil
}
func (w *Watcher) addWatchTree(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		name := info.Name()
		switch {
		case name == ".git" || name == "node_modules" || name == "rootfs":
			return filepath.SkipDir
		case strings.HasPrefix(name, ".") && name != "." && name != ".." &&
			!(path == root && name == ".trellis"):
			return filepath.SkipDir
		case name == "orin_bsp" || name == "qcom_bsp":
			return filepath.SkipDir
		case name == "argos-sdk" || name == "x5_sdk":
			return filepath.SkipDir
		case name == "mondo-ai":
			return filepath.SkipDir
		case (strings.Contains(name, "_sdk") || strings.Contains(name, "_bsp")) && filepath.Dir(path) != root:
			return filepath.SkipDir
		}
		if addErr := w.watcher.Add(path); addErr != nil {
			log.Printf("wiki watcher: failed to watch %s: %v", path, addErr)
		}
		return nil
	})
}

// AddPaths extends a running watcher after a workspace is registered.
func (w *Watcher) AddPaths(paths []string) {
	if w.watcher == nil {
		return
	}
	for _, root := range paths {
		w.addWatchTree(root)
	}
}

// Stop shuts down the watcher and blocks until its goroutine has exited.
func (w *Watcher) Stop() {
	close(w.stop)
	w.watcher.Close()
	w.wg.Wait()
}

func (w *Watcher) loop() {
	defer w.wg.Done()

	var pending []string
	var pendingNotified bool
	var timer *time.Timer
	var communityTimer *time.Timer

	resetTimer := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(w.debounce, func() {
			files := pending
			pending = nil
			pendingNotified = false
			if len(files) == 0 {
				return
			}
			w.processBatch(files)
			if w.api.DirtyCount() > communityDirtyThreshold {
				if communityTimer != nil {
					communityTimer.Stop()
				}
				communityTimer = time.AfterFunc(w.communityDelay, func() {
					w.redetectCommunities()
				})
			}
		})
	}

	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					w.addWatchTree(event.Name)
					if !pendingNotified && w.api.SSE != nil {
						w.api.SSE.BroadcastNamed("indexing-started", `{"changed":1}`)
						pendingNotified = true
					}
					pending = append(pending, event.Name)
					resetTimer()
					continue
				}
			}
			if !isWikiFile(event.Name) {
				continue
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if !pendingNotified && w.api.SSE != nil {
				w.api.SSE.BroadcastNamed("indexing-started", `{"changed":1}`)
				pendingNotified = true
			}
			pending = append(pending, event.Name)
			resetTimer()
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("wiki watcher error: %v", err)
		}
	}
}

// isWikiFile reports whether path is a source document or durable workflow
// metadata file that can change the graph.
func isWikiFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".md") ||
		base == ".comet.yaml" ||
		base == "task.json" ||
		base == "implement.jsonl" ||
		base == "check.jsonl" ||
		base == "debug.jsonl"
}

// processBatch handles a debounced batch with a per-file graph update. A full
// rebuild remains the correctness fallback if classification, embedding, link
// extraction, or cache persistence fails.
func (w *Watcher) processBatch(files []string) {
	log.Printf("wiki watcher: %d file(s) changed, updating index", len(files))
	if requiresFullRebuild(files) {
		if err := w.api.Rebuild(); err != nil {
			log.Printf("wiki watcher: Trellis rebuild failed: %v", err)
			return
		}
	} else if err := w.api.IncrementalUpdate(files); err != nil {
		log.Printf("wiki watcher: incremental update failed, falling back to full rebuild: %v", err)
		if rebuildErr := w.api.Rebuild(); rebuildErr != nil {
			log.Printf("wiki watcher: fallback rebuild failed: %v", rebuildErr)
			return
		}
	}
	if w.api.SSE != nil {
		w.api.SSE.Broadcast(fmt.Sprintf(`{"changed":%d}`, len(files)))
	}
	w.SyncMirror()
}

func requiresFullRebuild(files []string) bool {
	separator := string(filepath.Separator)
	trellisMarker := separator + ".trellis" + separator
	superpowersMarker := separator + filepath.Join("docs", "superpowers") + separator
	for _, path := range files {
		clean := filepath.Clean(path)
		if strings.Contains(clean, trellisMarker) || strings.Contains(clean, superpowersMarker) {
			return true
		}
	}
	return false
}

// SetMirror wires a knowledge-mirror sync target into the watcher. When
// set, every successful rebuild (whether triggered by a debounced file
// change or the initial startup scan) mirrors the complete current set of
// indexed components into the mirror's git repo.
func (w *Watcher) SetMirror(m *Mirror) {
	w.mirror = m
}

// SyncMirror mirrors the API's current graph contents into the configured
// Mirror (see SetMirror). It is safe to call even when no mirror is
// configured (no-op) or before any rebuild has run; callers include
// processBatch after every debounced rebuild and main.go after the initial
// startup rebuild.
func (w *Watcher) SyncMirror() {
	if w.mirror == nil {
		return
	}
	w.api.mu.RLock()
	components := w.api.graph.Components()
	ws := w.api.ws
	lister := w.api.lister
	w.api.mu.RUnlock()
	if lister != nil {
		ws = lister.List()
	}
	w.mirror.SyncAll(components, ws)
}

// redetectCommunities re-runs community detection (and re-derives
// community labels) on the current graph. It is called on a longer
// debounce than processBatch since Louvain community detection is more
// expensive than a single rescan and doesn't need to run on every edit.
func (w *Watcher) redetectCommunities() {
	if w.api.DirtyCount() <= communityDirtyThreshold {
		return
	}
	w.api.mu.Lock()
	defer w.api.mu.Unlock()
	g := w.api.graph
	g.SetCommunities(DetectCommunities(g))
	components := g.Components()
	comps := make([]Component, 0, len(components))
	for _, c := range components {
		comps = append(comps, c)
	}
	g.SetCommunityLabels(CommunityLabels(comps, g.Communities(), g.Embeddings()))
	w.api.ResetDirty()
	log.Printf("wiki watcher: communities re-detected")
}
