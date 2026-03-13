package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Version != "1" {
		t.Errorf("expected version '1', got %s", cfg.Version)
	}

	if len(cfg.Indexer.ExcludePatterns) == 0 {
		t.Error("expected default exclude patterns")
	}

	if cfg.Indexer.WatchDebounceMs != 500 {
		t.Errorf("expected default debounce 500ms, got %d", cfg.Indexer.WatchDebounceMs)
	}

	if !cfg.Indexer.AutoIndexOnStart {
		t.Error("expected auto_index_on_start to be true by default")
	}

	if cfg.MCP.Transport != "stdio" {
		t.Errorf("expected default transport 'stdio', got %s", cfg.MCP.Transport)
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("expected default log level 'info', got %s", cfg.Logging.Level)
	}
}

func TestLoadGlobalConfig(t *testing.T) {
	// Create a temporary directory for global config
	tempDir := t.TempDir()
	globalConfigPath := filepath.Join(tempDir, "config.json")

	configContent := `{
		"version": "2",
		"indexer": {
			"exclude_patterns": [".git", "custom"],
			"watch_debounce_ms": 1000
		}
	}`

	if err := os.WriteFile(globalConfigPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := &Loader{
		globalPath:  globalConfigPath,
		projectPath: "",
	}

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Version != "2" {
		t.Errorf("expected version '2', got %s", cfg.Version)
	}

	if len(cfg.Indexer.ExcludePatterns) != 2 {
		t.Errorf("expected 2 exclude patterns, got %d", len(cfg.Indexer.ExcludePatterns))
	}

	if cfg.Indexer.WatchDebounceMs != 1000 {
		t.Errorf("expected debounce 1000ms, got %d", cfg.Indexer.WatchDebounceMs)
	}
}

func TestProjectConfigOverridesGlobal(t *testing.T) {
	// Create temporary directory for configs
	tempDir := t.TempDir()
	globalConfigPath := filepath.Join(tempDir, "global.json")
	projectConfigPath := filepath.Join(tempDir, "project.json")

	globalContent := `{
		"version": "1",
		"indexer": {
			"exclude_patterns": [".git", "global_pattern"],
			"watch_debounce_ms": 500
		}
	}`

	projectContent := `{
		"indexer": {
			"exclude_patterns": [".git", "project_pattern"],
			"watch_debounce_ms": 1000
		}
	}`

	if err := os.WriteFile(globalConfigPath, []byte(globalContent), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}
	if err := os.WriteFile(projectConfigPath, []byte(projectContent), 0644); err != nil {
		t.Fatalf("failed to write project config: %v", err)
	}

	loader := &Loader{
		globalPath:  globalConfigPath,
		projectPath: projectConfigPath,
	}

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Project should override global
	if cfg.Indexer.WatchDebounceMs != 1000 {
		t.Errorf("expected debounce 1000ms (from project), got %d", cfg.Indexer.WatchDebounceMs)
	}

	// Check that project patterns are used (not merged)
	if len(cfg.Indexer.ExcludePatterns) != 2 {
		t.Errorf("expected 2 exclude patterns (from project), got %d", len(cfg.Indexer.ExcludePatterns))
	}

	// Check for project_pattern
	found := false
	for _, p := range cfg.Indexer.ExcludePatterns {
		if p == "project_pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'project_pattern' in exclude patterns")
	}
}

func TestOverridePathTakesPriority(t *testing.T) {
	tempDir := t.TempDir()
	globalConfigPath := filepath.Join(tempDir, "global.json")
	overrideConfigPath := filepath.Join(tempDir, "override.json")

	globalContent := `{
		"indexer": {
			"watch_debounce_ms": 500
		}
	}`

	overrideContent := `{
		"indexer": {
			"watch_debounce_ms": 2000
		}
	}`

	if err := os.WriteFile(globalConfigPath, []byte(globalContent), 0644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}
	if err := os.WriteFile(overrideConfigPath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("failed to write override config: %v", err)
	}

	loader := &Loader{
		globalPath:   globalConfigPath,
		projectPath:  "",
		overridePath: overrideConfigPath,
	}

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Indexer.WatchDebounceMs != 2000 {
		t.Errorf("expected debounce 2000ms (from override), got %d", cfg.Indexer.WatchDebounceMs)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	cfg := &Config{
		Version: "1",
		Indexer: IndexerConfig{
			ExcludePatterns:  []string{".git", "node_modules"},
			WatchDebounceMs:  750,
			AutoIndexOnStart: false,
		},
		MCP: MCPConfig{
			Transport: "stdio",
		},
		Logging: LoggingConfig{
			Level:  "debug",
			Format: "json",
		},
	}

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Load it back
	loader := &Loader{
		globalPath:  "",
		projectPath: configPath,
	}

	loadedCfg, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	if loadedCfg.Indexer.WatchDebounceMs != 750 {
		t.Errorf("expected debounce 750ms, got %d", loadedCfg.Indexer.WatchDebounceMs)
	}

	if loadedCfg.Indexer.AutoIndexOnStart {
		t.Error("expected auto_index_on_start to be false")
	}

	if loadedCfg.Logging.Level != "debug" {
		t.Errorf("expected log level 'debug', got %s", loadedCfg.Logging.Level)
	}
}

func TestGetWatchDebounce(t *testing.T) {
	cfg := &Config{
		Indexer: IndexerConfig{
			WatchDebounceMs: 0, // invalid, should return default
		},
	}

	if cfg.GetWatchDebounce() != 500 {
		t.Errorf("expected default 500ms for invalid value, got %d", cfg.GetWatchDebounce())
	}

	cfg.Indexer.WatchDebounceMs = 1000
	if cfg.GetWatchDebounce() != 1000 {
		t.Errorf("expected 1000ms, got %d", cfg.GetWatchDebounce())
	}
}

func TestGetMCPTransport(t *testing.T) {
	cfg := &Config{
		MCP: MCPConfig{
			Transport: "",
		},
	}

	if cfg.GetMCPTransport() != "stdio" {
		t.Errorf("expected default 'stdio', got %s", cfg.GetMCPTransport())
	}

	cfg.MCP.Transport = "http"
	if cfg.GetMCPTransport() != "http" {
		t.Errorf("expected 'http', got %s", cfg.GetMCPTransport())
	}
}
