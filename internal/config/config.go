// Package config provides configuration management for tuffman.
// Supports both global (user home) and project-level (.tuffman/) configuration files.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the complete tuffman configuration
type Config struct {
	Version string        `json:"version"`
	Indexer IndexerConfig `json:"indexer"`
	MCP     MCPConfig     `json:"mcp"`
	Logging LoggingConfig `json:"logging"`
}

// IndexerConfig holds indexer-specific configuration
type IndexerConfig struct {
	ExcludePatterns  []string `json:"exclude_patterns"`
	WatchDebounceMs  int      `json:"watch_debounce_ms"`
	AutoIndexOnStart bool     `json:"auto_index_on_start"`
}

// MCPConfig holds MCP server configuration
type MCPConfig struct {
	Transport string `json:"transport"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		Version: "1",
		Indexer: IndexerConfig{
			ExcludePatterns: []string{
				".git",
				".tuffman",
				"node_modules",
				"vendor",
				"__pycache__",
				"*.log",
				"*.tmp",
				"*.swp",
				".DS_Store",
			},
			WatchDebounceMs:  500,
			AutoIndexOnStart: true,
		},
		MCP: MCPConfig{
			Transport: "stdio",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// Loader handles loading configuration from multiple sources
type Loader struct {
	globalPath   string
	projectPath  string
	overridePath string
}

// NewLoader creates a new config loader
func NewLoader(projectRoot string) *Loader {
	homeDir, _ := os.UserHomeDir()

	return &Loader{
		globalPath:   filepath.Join(homeDir, ".config", "tuffman", "config.json"),
		projectPath:  filepath.Join(projectRoot, ".tuffman", "config.json"),
		overridePath: "",
	}
}

// SetOverridePath sets a config path that takes highest priority (from --config flag)
func (l *Loader) SetOverridePath(path string) {
	l.overridePath = path
}

// Load loads and merges configuration from all available sources
// Priority (highest to lowest): override > project > global > defaults
func (l *Loader) Load() (*Config, error) {
	config := DefaultConfig()

	// Load global config if exists
	if l.globalPath != "" && fileExists(l.globalPath) {
		if err := l.loadFile(l.globalPath, config); err != nil {
			return nil, fmt.Errorf("loading global config: %w", err)
		}
	}

	// Load project config if exists (overrides global)
	if l.projectPath != "" && fileExists(l.projectPath) {
		if err := l.loadFile(l.projectPath, config); err != nil {
			return nil, fmt.Errorf("loading project config: %w", err)
		}
	}

	// Load override config if specified (highest priority)
	if l.overridePath != "" && fileExists(l.overridePath) {
		if err := l.loadFile(l.overridePath, config); err != nil {
			return nil, fmt.Errorf("loading override config: %w", err)
		}
	}

	return config, nil
}

// loadFile loads a config file and merges it with the existing config
func (l *Loader) loadFile(path string, config *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var fileConfig Config
	if err := json.Unmarshal(data, &fileConfig); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	// Merge file config into current config
	mergeConfig(config, &fileConfig)

	return nil
}

// mergeConfig merges source config into target config
// Only non-zero values from source are applied
func mergeConfig(target, source *Config) {
	if source.Version != "" {
		target.Version = source.Version
	}

	// Merge indexer config
	if len(source.Indexer.ExcludePatterns) > 0 {
		target.Indexer.ExcludePatterns = source.Indexer.ExcludePatterns
	}
	if source.Indexer.WatchDebounceMs > 0 {
		target.Indexer.WatchDebounceMs = source.Indexer.WatchDebounceMs
	}
	// AutoIndexOnStart is a bool, always apply if explicitly set in file
	// We use the value as-is since bool defaults to false
	target.Indexer.AutoIndexOnStart = source.Indexer.AutoIndexOnStart

	// Merge MCP config
	if source.MCP.Transport != "" {
		target.MCP.Transport = source.MCP.Transport
	}

	// Merge logging config
	if source.Logging.Level != "" {
		target.Logging.Level = source.Logging.Level
	}
	if source.Logging.Format != "" {
		target.Logging.Format = source.Logging.Format
	}
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Save saves the configuration to the specified path
func Save(path string, config *Config) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	// Add trailing newline
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// GetGlobalConfigPath returns the path to the global config file
func GetGlobalConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "tuffman", "config.json")
}

// GetProjectConfigPath returns the path to the project config file
func GetProjectConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".tuffman", "config.json")
}

// InitProjectConfig creates a new project-level config file with default values
func InitProjectConfig(projectRoot string) (string, error) {
	config := DefaultConfig()
	path := GetProjectConfigPath(projectRoot)

	if err := Save(path, config); err != nil {
		return "", err
	}

	return path, nil
}

// InitGlobalConfig creates a new global config file with default values
func InitGlobalConfig() (string, error) {
	config := DefaultConfig()
	path := GetGlobalConfigPath()

	if err := Save(path, config); err != nil {
		return "", err
	}

	return path, nil
}
