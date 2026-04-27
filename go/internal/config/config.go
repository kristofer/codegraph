// Package config manages CodeGraph project configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kristofer/codegraph/internal/types"
)

// DirName is the name of the per-project CodeGraph directory.
const DirName = ".codegraph"

// FileName is the name of the configuration file inside DirName.
const FileName = "config.json"

// FrameworkHint provides framework-specific hints for better extraction.
type FrameworkHint struct {
	// Name is the framework name (react, express, django, etc.).
	Name string `json:"name"`

	// Version is an optional version constraint.
	Version string `json:"version,omitempty"`

	// Patterns holds optional custom detection patterns.
	Patterns *FrameworkPatterns `json:"patterns,omitempty"`
}

// FrameworkPatterns holds framework-specific detection patterns.
type FrameworkPatterns struct {
	Components []string `json:"components,omitempty"`
	Routes     []string `json:"routes,omitempty"`
	Models     []string `json:"models,omitempty"`
}

// CustomPattern is a user-defined symbol extraction pattern.
type CustomPattern struct {
	// Name is a label for this pattern group.
	Name string `json:"name"`

	// Pattern is the regex pattern to match.
	Pattern string `json:"pattern"`

	// Kind is the NodeKind to assign to matched symbols.
	Kind types.NodeKind `json:"kind"`
}

// Config is the configuration for a CodeGraph project.
// It mirrors the TypeScript CodeGraphConfig interface.
type Config struct {
	// Version is the schema version for migrations.
	Version int `json:"version"`

	// RootDir is the root directory of the project.
	RootDir string `json:"rootDir"`

	// Include holds glob patterns for files to include.
	Include []string `json:"include"`

	// Exclude holds glob patterns for files to exclude.
	Exclude []string `json:"exclude"`

	// Languages lists languages to process (auto-detected if empty).
	Languages []types.Language `json:"languages"`

	// Frameworks holds framework hints for better extraction.
	Frameworks []FrameworkHint `json:"frameworks"`

	// MaxFileSize is the maximum file size to process (bytes).
	MaxFileSize int64 `json:"maxFileSize"`

	// ExtractDocstrings controls whether docstrings are extracted.
	ExtractDocstrings bool `json:"extractDocstrings"`

	// TrackCallSites controls whether call sites are tracked.
	TrackCallSites bool `json:"trackCallSites"`

	// CustomPatterns holds user-defined symbol extraction patterns.
	CustomPatterns []CustomPattern `json:"customPatterns,omitempty"`
}

// DefaultConfig returns the default configuration.
// It mirrors the DEFAULT_CONFIG constant from the TypeScript source.
func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		RootDir: ".",
		Include: []string{
			// TypeScript/JavaScript
			"**/*.ts",
			"**/*.tsx",
			"**/*.js",
			"**/*.jsx",
			// Python
			"**/*.py",
			// Go
			"**/*.go",
			// Rust
			"**/*.rs",
			// Java
			"**/*.java",
			// C/C++
			"**/*.c",
			"**/*.h",
			"**/*.cpp",
			"**/*.hpp",
			"**/*.cc",
			"**/*.cxx",
			// C#
			"**/*.cs",
			// PHP
			"**/*.php",
			// Ruby
			"**/*.rb",
			// Swift
			"**/*.swift",
			// Kotlin
			"**/*.kt",
			"**/*.kts",
			// Dart
			"**/*.dart",
			// Svelte
			"**/*.svelte",
			// Liquid (Shopify themes)
			"**/*.liquid",
			// Pascal / Delphi
			"**/*.pas",
			"**/*.dpr",
			"**/*.dpk",
			"**/*.lpr",
			"**/*.dfm",
			"**/*.fmx",
		},
		Exclude: []string{
			// Version control
			"**/.git/**",

			// Dependencies
			"**/node_modules/**",
			"**/vendor/**",
			"**/Pods/**",

			// Generic build outputs
			"**/dist/**",
			"**/build/**",
			"**/out/**",
			"**/bin/**",
			"**/obj/**",
			"**/target/**",

			// JavaScript/TypeScript
			"**/*.min.js",
			"**/*.bundle.js",
			"**/.next/**",
			"**/.nuxt/**",
			"**/.svelte-kit/**",
			"**/.output/**",
			"**/.turbo/**",
			"**/.cache/**",
			"**/.parcel-cache/**",
			"**/.vite/**",
			"**/.astro/**",
			"**/.docusaurus/**",
			"**/.gatsby/**",
			"**/.webpack/**",
			"**/.nx/**",
			"**/.yarn/cache/**",
			"**/.pnpm-store/**",
			"**/storybook-static/**",

			// React Native / Expo
			"**/.expo/**",
			"**/web-build/**",
			"**/ios/Pods/**",
			"**/ios/build/**",
			"**/android/build/**",
			"**/android/.gradle/**",

			// Python
			"**/__pycache__/**",
			"**/.venv/**",
			"**/venv/**",
			"**/site-packages/**",
			"**/dist-packages/**",
			"**/.pytest_cache/**",
			"**/.mypy_cache/**",
			"**/.ruff_cache/**",
			"**/.tox/**",
			"**/.nox/**",
			"**/*.egg-info/**",
			"**/.eggs/**",

			// Go
			"**/go/pkg/mod/**",

			// Rust
			"**/target/debug/**",
			"**/target/release/**",

			// Java/Kotlin/Gradle
			"**/.gradle/**",
			"**/.m2/**",
			"**/generated-sources/**",
			"**/.kotlin/**",

			// Dart/Flutter
			"**/.dart_tool/**",

			// C#/.NET
			"**/.vs/**",
			"**/.nuget/**",
			"**/artifacts/**",
			"**/publish/**",

			// C/C++
			"**/cmake-build-*/**",
			"**/CMakeFiles/**",
			"**/bazel-*/**",
			"**/vcpkg_installed/**",
			"**/.conan/**",
			"**/Debug/**",
			"**/Release/**",
			"**/x64/**",
			"**/.pio/**",

			// Electron
			"**/release/**",
			"**/*.app/**",
			"**/*.asar",

			// Swift/iOS/Xcode
			"**/DerivedData/**",
			"**/.build/**",
			"**/.swiftpm/**",
			"**/xcuserdata/**",
			"**/Carthage/Build/**",
			"**/SourcePackages/**",

			// Delphi/Pascal
			"**/__history/**",
			"**/__recovery/**",
			"**/*.dcu",

			// PHP
			"**/.composer/**",
			"**/storage/framework/**",
			"**/bootstrap/cache/**",

			// Ruby
			"**/.bundle/**",
			"**/tmp/cache/**",
			"**/public/assets/**",
			"**/public/packs/**",
			"**/.yardoc/**",

			// Testing/Coverage
			"**/coverage/**",
			"**/htmlcov/**",
			"**/.nyc_output/**",
			"**/test-results/**",
			"**/.coverage/**",

			// IDE/Editor
			"**/.idea/**",

			// Logs and temp
			"**/logs/**",
			"**/tmp/**",
			"**/temp/**",

			// Documentation build output
			"**/_build/**",
			"**/docs/_build/**",
			"**/site/**",
		},
		Languages:         []types.Language{},
		Frameworks:        []FrameworkHint{},
		MaxFileSize:       1024 * 1024, // 1 MB
		ExtractDocstrings: true,
		TrackCallSites:    true,
	}
}

// ConfigPath returns the absolute path to the config file for the given project root.
func ConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, DirName, FileName)
}

// LoadConfig reads the config file from the given project root directory.
// If no config file exists, ErrNotInitialized is returned.
// Values present in the file override the defaults; missing fields retain their defaults.
func LoadConfig(projectRoot string) (*Config, error) {
	cfgPath := ConfigPath(projectRoot)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: config not found at %s", types.ErrNotInitialized, cfgPath)
		}
		return nil, fmt.Errorf("codegraph: reading config: %w", err)
	}

	// Start from defaults so missing keys are filled in.
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("codegraph: parsing config: %w", err)
	}

	return cfg, nil
}

// SaveConfig writes the configuration to the .codegraph/config.json file inside
// the given project root. The directory is created if it does not exist.
func SaveConfig(projectRoot string, cfg *Config) error {
	dir := filepath.Join(projectRoot, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("codegraph: creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("codegraph: marshalling config: %w", err)
	}

	cfgPath := filepath.Join(dir, FileName)
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return fmt.Errorf("codegraph: writing config: %w", err)
	}

	return nil
}
