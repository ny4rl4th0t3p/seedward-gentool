package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRootCmdReturnsFreshInstances(t *testing.T) {
	a, b := NewRootCmd(), NewRootCmd()
	require.NotSame(t, a, b)

	require.NoError(t, a.PersistentFlags().Set(flagConfig, "a.yaml"))
	got, err := b.PersistentFlags().GetString(flagConfig)
	require.NoError(t, err)
	require.Empty(t, got, "flag state must not be shared between instances")
}

func TestNewGenesisCommandsReturnsFreshInstances(t *testing.T) {
	a, b := NewGenesisCommands(), NewGenesisCommands()
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	require.NotSame(t, a[0], b[0])

	require.NoError(t, a[0].Flags().Set(flagInputGenesis, "x.json"))
	got, err := b[0].Flags().GetString(flagInputGenesis)
	require.NoError(t, err)
	require.Empty(t, got, "flag state must not be shared between instances")
}
