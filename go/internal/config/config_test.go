package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristofer/codegraph/internal/types"
)

// TestDefaultConfig verifies that DefaultConfig returns a sensible configuration.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, ".", cfg.RootDir)
	assert.True(t, cfg.ExtractDocstrings)
	assert.True(t, cfg.TrackCallSites)
	assert.Equal(t, int64(1024*1024), cfg.MaxFileSize)
	assert.NotEmpty(t, cfg.Include, "Include patterns must not be empty")
	assert.NotEmpty(t, cfg.Exclude, "Exclude patterns must not be empty")
	assert.NotNil(t, cfg.Languages)
	assert.NotNil(t, cfg.Frameworks)

	// Spot-check a few well-known include patterns
	assert.Contains(t, cfg.Include, "**/*.ts")
	assert.Contains(t, cfg.Include, "**/*.go")
	assert.Contains(t, cfg.Include, "**/*.py")

	// Spot-check a few well-known exclude patterns
	assert.Contains(t, cfg.Exclude, "**/node_modules/**")
	assert.Contains(t, cfg.Exclude, "**/.git/**")
}

// TestConfigLoadSave verifies that a config can be saved and loaded round-trip.
func TestConfigLoadSave(t *testing.T) {
	dir := t.TempDir()

	original := DefaultConfig()
	original.RootDir = "/my/project"
	original.MaxFileSize = 512 * 1024
	original.Languages = []types.Language{types.TypeScript, types.Go}
	original.Frameworks = []FrameworkHint{{Name: "react", Version: "18"}}

	require.NoError(t, SaveConfig(dir, original))

	// Verify the file was created
	cfgPath := ConfigPath(dir)
	info, err := os.Stat(cfgPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))

	// Load it back
	loaded, err := LoadConfig(dir)
	require.NoError(t, err)

	assert.Equal(t, original.Version, loaded.Version)
	assert.Equal(t, original.RootDir, loaded.RootDir)
	assert.Equal(t, original.MaxFileSize, loaded.MaxFileSize)
	assert.Equal(t, original.Languages, loaded.Languages)
	require.Len(t, loaded.Frameworks, 1)
	assert.Equal(t, "react", loaded.Frameworks[0].Name)
	assert.Equal(t, "18", loaded.Frameworks[0].Version)
}

// TestConfigLoadNotFound verifies that loading from an un-initialized directory
// returns ErrNotInitialized.
func TestConfigLoadNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadConfig(dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, types.ErrNotInitialized)
}

// TestConfigMergeDefaults verifies that fields not present in the JSON file
// retain their default values after load.
func TestConfigMergeDefaults(t *testing.T) {
	dir := t.TempDir()

	// Write a partial JSON config — only override MaxFileSize.
	partial := map[string]any{
		"version":     1,
		"maxFileSize": 999,
	}
	data, err := json.MarshalIndent(partial, "", "  ")
	require.NoError(t, err)

	cfgDir := filepath.Join(dir, DirName)
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, FileName), data, 0o644))

	loaded, err := LoadConfig(dir)
	require.NoError(t, err)

	// Overridden field
	assert.Equal(t, int64(999), loaded.MaxFileSize)

	// Default fields should still be populated
	assert.NotEmpty(t, loaded.Include)
	assert.NotEmpty(t, loaded.Exclude)
	assert.True(t, loaded.ExtractDocstrings)
}

// TestConfigPath verifies the helper returns the expected file path.
func TestConfigPath(t *testing.T) {
	got := ConfigPath("/my/project")
	assert.Equal(t, filepath.Join("/my/project", DirName, FileName), got)
}
