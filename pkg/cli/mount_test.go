package cli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// executeUnderParent mounts NewGenesisCommands under an arbitrary host CLI
// (the spec's seedward reference shape) and executes the given args.
func executeUnderParent(t *testing.T, args ...string) error {
	t.Helper()
	genesisCmd := &cobra.Command{Use: "genesis", Short: "Genesis construction (gentool)"}
	for _, c := range NewGenesisCommands() {
		genesisCmd.AddCommand(c)
	}
	parent := &cobra.Command{Use: "seedward"}
	parent.AddCommand(genesisCmd)

	var buf bytes.Buffer
	parent.SetOut(&buf)
	parent.SetErr(&buf)
	parent.SetArgs(args)
	return parent.Execute()
}

func TestMountedCreateRequiresInputGenesisFlag(t *testing.T) {
	err := executeUnderParent(t, "genesis", "create")
	require.ErrorContains(t, err, "--input-genesis is required")
}

func TestMountedCreateReadsConfigFlagWithoutGentoolRoot(t *testing.T) {
	// Reaching the --input-genesis read error proves --config was parsed and
	// the config file loaded: chain.address_prefix is checked first and only
	// exists in the config file.
	err := executeUnderParent(t,
		"genesis", "create",
		"--config", "testdata/test-config.yaml",
		"--input-genesis", "testdata/does-not-exist.json",
	)
	require.ErrorContains(t, err, "failed to read --input-genesis")
}

func TestMountedCreateFailsWithoutConfig(t *testing.T) {
	err := executeUnderParent(t,
		"genesis", "create",
		"--input-genesis", "testdata/does-not-exist.json",
	)
	require.ErrorContains(t, err, "chain.address_prefix is required in config")
}

// TestRootConfigBeforeSubcommand guards the alias of create's --config flag on
// gentool's root: the historical `gentool --config x create` spelling must keep
// setting the same flag the create command reads.
func TestRootConfigBeforeSubcommand(t *testing.T) {
	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--config", "testdata/test-config.yaml",
		"create",
		"--input-genesis", "testdata/does-not-exist.json",
	})
	err := root.Execute()
	require.ErrorContains(t, err, "failed to read --input-genesis")
}
