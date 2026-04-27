package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVersion is a smoke test that verifies the binary compiles and prints a
// version string without panicking.
func TestVersion(t *testing.T) {
	// Build the binary into a temp directory.
	tmpDir := t.TempDir()
	binPath := tmpDir + "/codegraph"

	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildOut, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", buildOut)

	// Run with --version flag.
	runCmd := exec.Command(binPath, "--version")
	out, err := runCmd.CombinedOutput()
	require.NoError(t, err, "codegraph --version failed: %s", out)

	output := strings.TrimSpace(string(out))
	assert.Contains(t, output, "codegraph", "version output should contain 'codegraph'")
}
