package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update-golden", false, "rewrite golden files with current output")

// executeRoot runs a fresh root command with args and returns its combined output.
func executeRoot(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	require.NoError(t, root.Execute())
	return buf.String()
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), got,
		"CLI surface drifted from %s; run `go test ./pkg/cli -update-golden` if the change is intentional", path)
}

// TestSurfaceGolden locks gentool's user-visible CLI surface: command tree,
// flags, and help text.
func TestSurfaceGolden(t *testing.T) {
	checkGolden(t, "root-help.golden", executeRoot(t, "--help"))
	checkGolden(t, "create-help.golden", executeRoot(t, "create", "--help"))
}

func TestVersionString(t *testing.T) {
	require.Equal(t, "gentool version dev\n", executeRoot(t, "--version"))
}
