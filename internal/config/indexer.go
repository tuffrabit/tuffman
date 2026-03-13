package config

import (
	"time"

	"github.com/tuffrabit/tuffman/internal/indexer"
	"github.com/tuffrabit/tuffman/internal/watcher"
)

// ToIndexerConfig creates an indexer.Config from the file-based config
// This allows the indexer to use configuration from the config file
func (c *Config) ToIndexerConfig(root string) *indexer.Config {
	// Start with the default indexer config (which includes git root detection)
	idxConfig := indexer.DefaultConfig(root)

	// Override with file-based exclude patterns if specified
	if len(c.Indexer.ExcludePatterns) > 0 {
		idxConfig.ExcludePatterns = c.Indexer.ExcludePatterns
	}

	return idxConfig
}

// ToWatcherConfig creates a watcher.Config from the file-based config
func (c *Config) ToWatcherConfig(idxConfig *indexer.Config) *watcher.Config {
	exts := make(map[string]struct{})
	for ext := range idxConfig.IncludeExtensions {
		exts[ext] = struct{}{}
	}

	debounceMs := c.Indexer.WatchDebounceMs
	if debounceMs <= 0 {
		debounceMs = 500
	}

	return &watcher.Config{
		Root:                idxConfig.Root,
		ExcludePatterns:     idxConfig.ExcludePatterns,
		SupportedExtensions: exts,
		DebounceDelay:       time.Duration(debounceMs) * time.Millisecond,
		MaxRetries:          3,
		RetryDelay:          1 * time.Second,
	}
}

// GetWatchDebounce returns the configured watch debounce duration in milliseconds
func (c *Config) GetWatchDebounce() int {
	if c.Indexer.WatchDebounceMs <= 0 {
		return 500 // default
	}
	return c.Indexer.WatchDebounceMs
}

// ShouldAutoIndexOnStart returns whether auto-indexing is enabled on server start
func (c *Config) ShouldAutoIndexOnStart() bool {
	return c.Indexer.AutoIndexOnStart
}
