package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tuffrabit/tuffman/internal/indexer"
)

// EventType represents the type of file system event
type EventType int

const (
	EventCreate EventType = iota
	EventModify
	EventDelete
)

// Event represents a file system event
type Event struct {
	Path  string
	Type  EventType
	IsDir bool
}

// Config holds watcher configuration
type Config struct {
	// Root directory to watch
	Root string

	// ExcludePatterns are patterns for files/directories to exclude
	ExcludePatterns []string

	// SupportedExtensions maps extensions to languages
	SupportedExtensions map[string]struct{}

	// DebounceDelay is the delay before processing events
	DebounceDelay time.Duration

	// MaxRetries is the maximum number of retries for failed indexing
	MaxRetries int

	// RetryDelay is the delay between retries
	RetryDelay time.Duration
}

// DefaultConfig returns a default watcher configuration
func DefaultConfig(root string, indexerConfig *indexer.Config) *Config {
	exts := make(map[string]struct{})
	for ext := range indexerConfig.IncludeExtensions {
		exts[ext] = struct{}{}
	}

	return &Config{
		Root:                root,
		ExcludePatterns:     indexerConfig.ExcludePatterns,
		SupportedExtensions: exts,
		DebounceDelay:       500 * time.Millisecond,
		MaxRetries:          3,
		RetryDelay:          1 * time.Second,
	}
}

// Handler is called when files need to be re-indexed
type Handler interface {
	// IndexFiles indexes a batch of files
	IndexFiles(paths []string) error

	// DeleteFile removes a file from the index
	DeleteFile(path string) error

	// IndexAll performs a full re-index
	IndexAll(ctx context.Context) error
}

// Watcher watches a directory for file changes and triggers indexing
type Watcher struct {
	config    *Config
	handler   Handler
	fsWatcher *fsnotify.Watcher

	// Events are sent to this channel
	events chan Event

	// Pending changes are accumulated here
	pending   map[string]EventType
	pendingMu sync.Mutex

	// Debounce timer
	timer   *time.Timer
	timerMu sync.Mutex

	// In-flight operation cancellation
	cancel   context.CancelFunc
	cancelMu sync.Mutex

	// Git HEAD path for branch detection
	gitHeadPath string

	// Stop channel
	stopCh chan struct{}
}

// New creates a new Watcher
func New(config *Config, handler Handler) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	w := &Watcher{
		config:    config,
		handler:   handler,
		fsWatcher: fsWatcher,
		events:    make(chan Event, 100),
		pending:   make(map[string]EventType),
		stopCh:    make(chan struct{}),
	}

	// Find git HEAD path if in a git repo
	w.gitHeadPath = w.findGitHead(config.Root)

	return w, nil
}

// findGitHead searches for .git/HEAD starting from root
func (w *Watcher) findGitHead(root string) string {
	dir := root
	for {
		gitHead := filepath.Join(dir, ".git", "HEAD")
		if info, err := os.Stat(gitHead); err == nil && !info.IsDir() {
			return gitHead
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// Start begins watching the directory
func (w *Watcher) Start(ctx context.Context) error {
	// Add the root directory and all subdirectories to watch
	if err := w.addWatches(); err != nil {
		return fmt.Errorf("adding watches: %w", err)
	}

	// Start event processor
	go w.processEvents(ctx)

	// Start fsnotify event handler
	go w.handleFsEvents(ctx)

	return nil
}

// addWatches recursively adds directories to watch
func (w *Watcher) addWatches() error {
	return filepath.WalkDir(w.config.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded paths
		if w.shouldExclude(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only watch directories
		if !d.IsDir() {
			return nil
		}

		if err := w.fsWatcher.Add(path); err != nil {
			// Log but don't fail - some directories might not be watchable
			fmt.Fprintf(os.Stderr, "Warning: could not watch %s: %v\n", path, err)
		}

		return nil
	})
}

// shouldExclude checks if a path should be excluded
func (w *Watcher) shouldExclude(path string, d os.DirEntry) bool {
	rel, err := filepath.Rel(w.config.Root, path)
	if err != nil {
		return true
	}

	base := filepath.Base(path)

	for _, pattern := range w.config.ExcludePatterns {
		// Direct match
		if base == pattern {
			return true
		}

		// Glob match
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}

		// Path component match for directories
		if d.IsDir() && strings.Contains(rel, string(filepath.Separator)+pattern+string(filepath.Separator)) {
			return true
		}
		if strings.HasPrefix(rel, pattern+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

// handleFsEvents processes events from fsnotify
func (w *Watcher) handleFsEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "Watcher error: %v\n", err)
		}
	}
}

// handleEvent processes a single fsnotify event
func (w *Watcher) handleEvent(event fsnotify.Event) {
	path := event.Name

	// Check if this is .git/HEAD change (branch switch)
	if w.gitHeadPath != "" && path == w.gitHeadPath {
		if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
			fmt.Println("Git branch change detected, triggering full re-index...")
			w.triggerFullReindex()
			return
		}
	}

	// Get file info
	info, err := os.Stat(path)
	isDir := err == nil && info.IsDir()

	// Handle directory creation - add it to watch
	if isDir && event.Has(fsnotify.Create) {
		// Check if this directory should be excluded
		rel, err := filepath.Rel(w.config.Root, path)
		if err != nil {
			return
		}
		base := filepath.Base(path)
		shouldExclude := false
		for _, pattern := range w.config.ExcludePatterns {
			if base == pattern {
				shouldExclude = true
				break
			}
			if matched, _ := filepath.Match(pattern, base); matched {
				shouldExclude = true
				break
			}
			if strings.HasPrefix(rel, pattern+string(filepath.Separator)) {
				shouldExclude = true
				break
			}
		}
		if !shouldExclude {
			if err := w.fsWatcher.Add(path); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not watch new directory %s: %v\n", path, err)
			}
		}
		return
	}

	// Check if file extension is supported
	ext := strings.ToLower(filepath.Ext(path))
	if _, supported := w.config.SupportedExtensions[ext]; !supported {
		return
	}

	// Skip excluded files
	// Check exclusion using file info
	if err == nil {
		rel, relErr := filepath.Rel(w.config.Root, path)
		if relErr != nil {
			return
		}
		base := filepath.Base(path)
		for _, pattern := range w.config.ExcludePatterns {
			if base == pattern {
				return
			}
			if matched, _ := filepath.Match(pattern, base); matched {
				return
			}
			if strings.Contains(rel, string(filepath.Separator)+pattern+string(filepath.Separator)) {
				return
			}
			if strings.HasPrefix(rel, pattern+string(filepath.Separator)) {
				return
			}
		}
	}

	// Determine event type
	var eventType EventType
	switch {
	case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
		eventType = EventDelete
	case event.Has(fsnotify.Create):
		eventType = EventCreate
	case event.Has(fsnotify.Write):
		eventType = EventModify
	default:
		return
	}

	// Queue the event
	w.events <- Event{
		Path:  path,
		Type:  eventType,
		IsDir: isDir,
	}
}

// processEvents handles the debouncing and batching of events
func (w *Watcher) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return

		case event := <-w.events:
			w.pendingMu.Lock()

			// Add to pending changes
			w.pending[event.Path] = event.Type

			// Cancel any in-flight operation
			w.cancelMu.Lock()
			if w.cancel != nil {
				w.cancel()
				w.cancel = nil
			}
			w.cancelMu.Unlock()

			w.pendingMu.Unlock()

			// Reset debounce timer
			w.resetTimer()
		}
	}
}

// resetTimer resets the debounce timer
func (w *Watcher) resetTimer() {
	w.timerMu.Lock()
	defer w.timerMu.Unlock()

	if w.timer != nil {
		w.timer.Stop()
	}

	w.timer = time.AfterFunc(w.config.DebounceDelay, w.processPending)
}

// processPending processes all pending changes
func (w *Watcher) processPending() {
	w.pendingMu.Lock()

	// Copy and clear pending
	pending := make(map[string]EventType, len(w.pending))
	for k, v := range w.pending {
		pending[k] = v
	}
	w.pending = make(map[string]EventType)

	// Create new context for this operation
	ctx, cancel := context.WithCancel(context.Background())
	w.cancelMu.Lock()
	w.cancel = cancel
	w.cancelMu.Unlock()

	w.pendingMu.Unlock()

	// Process the batch
	if err := w.processBatch(ctx, pending); err != nil {
		if err != context.Canceled {
			fmt.Fprintf(os.Stderr, "Error processing batch: %v\n", err)
		}
	}

	// Clear cancel function
	w.cancelMu.Lock()
	w.cancel = nil
	w.cancelMu.Unlock()
}

// processBatch processes a batch of pending changes
func (w *Watcher) processBatch(ctx context.Context, pending map[string]EventType) error {
	var toIndex []string
	var toDelete []string

	for path, eventType := range pending {
		// Check if operation was cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch eventType {
		case EventDelete:
			toDelete = append(toDelete, path)
		case EventCreate, EventModify:
			// Check if file still exists (might have been deleted quickly)
			if _, err := os.Stat(path); err == nil {
				toIndex = append(toIndex, path)
			}
		}
	}

	// Process deletions
	for _, path := range toDelete {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := w.handler.DeleteFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting %s from index: %v\n", path, err)
		} else {
			fmt.Printf("Removed from index: %s\n", path)
		}
	}

	// Process indexing with retries
	if len(toIndex) > 0 {
		if err := w.indexWithRetry(ctx, toIndex); err != nil {
			if err != context.Canceled {
				fmt.Fprintf(os.Stderr, "Failed to index files after %d retries: %v\n", w.config.MaxRetries, err)
				// Clear old symbols as fallback
				for _, path := range toIndex {
					if clearErr := w.handler.DeleteFile(path); clearErr != nil {
						fmt.Fprintf(os.Stderr, "Error clearing %s from index: %v\n", path, clearErr)
					}
				}
			}
		}
	}

	return nil
}

// indexWithRetry attempts to index files with retries
func (w *Watcher) indexWithRetry(ctx context.Context, paths []string) error {
	var lastErr error

	for attempt := 0; attempt < w.config.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if attempt > 0 {
			fmt.Printf("Retrying index (attempt %d/%d)...\n", attempt+1, w.config.MaxRetries)
			time.Sleep(w.config.RetryDelay)
		}

		err := w.handler.IndexFiles(paths)
		if err == nil {
			for _, path := range paths {
				fmt.Printf("Indexed: %s\n", path)
			}
			return nil
		}

		lastErr = err
		fmt.Fprintf(os.Stderr, "Index attempt %d failed: %v\n", attempt+1, err)
	}

	return lastErr
}

// triggerFullReindex triggers a full re-index
func (w *Watcher) triggerFullReindex() {
	// Cancel any in-flight operation
	w.cancelMu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.cancelMu.Unlock()

	// Clear pending changes
	w.pendingMu.Lock()
	w.pending = make(map[string]EventType)
	w.pendingMu.Unlock()

	// Run full re-index in background
	go func() {
		ctx := context.Background()
		fmt.Println("Starting full re-index...")
		if err := w.handler.IndexAll(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Full re-index failed: %v\n", err)
		} else {
			fmt.Println("Full re-index completed")
		}
	}()
}

// Stop stops the watcher
func (w *Watcher) Stop() error {
	close(w.stopCh)

	w.timerMu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timerMu.Unlock()

	return w.fsWatcher.Close()
}
