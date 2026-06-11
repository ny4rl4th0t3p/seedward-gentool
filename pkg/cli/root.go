// Package cli constructs gentool's command tree. Every command comes from a
// constructor returning a fresh, self-contained instance, so the genesis
// commands can be mounted under a host CLI (e.g. seedward) as well as
// gentool's own root.
package cli

import (
	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags="-X 'github.com/ny4rl4th0t3p/seedward-gentool/pkg/cli.Version=<tag>'".
var Version = "dev"

// NewRootCmd returns gentool's full root command, freshly constructed on each call.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "gentool",
		Version: Version,
		Short:   "Generate a Cosmos SDK genesis file from CSV inputs",
		Long:    `gentool builds a genesis file for any Cosmos SDK chain from a baseline genesis and CSV-defined accounts, claims, and grants.`,
	}

	genesisCmds := NewGenesisCommands()
	for _, c := range genesisCmds {
		root.AddCommand(c)
	}

	// The create command owns --config so it stays self-contained under any
	// parent; aliasing the same *pflag.Flag object here keeps the historical
	// `gentool --config x create` spelling and the root-help listing working.
	// AddFlag panics on duplicate names, so alias it from one command only.
	root.PersistentFlags().AddFlag(genesisCmds[0].Flags().Lookup(flagConfig))

	return root
}
