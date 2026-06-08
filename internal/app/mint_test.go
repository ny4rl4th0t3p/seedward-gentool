package app

import (
	"encoding/json"
	"testing"

	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/encoding"
)

func mintAppState(t *testing.T) (map[string]json.RawMessage, encoding.EncodingConfig) {
	t.Helper()
	ec := encoding.NewEncodingConfig()
	gs := minttypes.DefaultGenesisState()
	bz, err := ec.Codec.MarshalJSON(gs)
	require.NoError(t, err)
	return map[string]json.RawMessage{"mint": bz}, ec
}

func readMintState(t *testing.T, appGenState map[string]json.RawMessage, ec encoding.EncodingConfig) *minttypes.GenesisState {
	t.Helper()
	var gs minttypes.GenesisState
	require.NoError(t, ec.Codec.UnmarshalJSON(appGenState["mint"], &gs))
	return &gs
}

func TestFixMintParameters_SetsMintDenom(t *testing.T) {
	appGenState, ec := mintAppState(t)
	asm := StateManager{encodingConfig: ec, cfg: ChainConfig{BondDenom: "ustake"}}
	require.NoError(t, asm.fixMintParameters(appGenState))

	gs := readMintState(t, appGenState, ec)
	assert.Equal(t, "ustake", gs.Params.MintDenom)
}

func TestFixMintParameters_BlocksPerYear(t *testing.T) {
	appGenState, ec := mintAppState(t)
	asm := StateManager{encodingConfig: ec, cfg: ChainConfig{BondDenom: "uatom", BlocksPerYear: 6_000_000}}
	require.NoError(t, asm.fixMintParameters(appGenState))

	gs := readMintState(t, appGenState, ec)
	assert.Equal(t, uint64(6_000_000), gs.Params.BlocksPerYear)
}

func TestFixMintParameters_InflationParams(t *testing.T) {
	appGenState, ec := mintAppState(t)
	asm := StateManager{encodingConfig: ec, cfg: ChainConfig{
		BondDenom:           "uatom",
		InflationRateChange: "0.13",
		InflationMax:        "0.20",
		InflationMin:        "0.07",
		GoalBonded:          "0.67",
	}}
	require.NoError(t, asm.fixMintParameters(appGenState))

	gs := readMintState(t, appGenState, ec)
	assert.Equal(t, "0.130000000000000000", gs.Params.InflationRateChange.String())
	assert.Equal(t, "0.200000000000000000", gs.Params.InflationMax.String())
	assert.Equal(t, "0.070000000000000000", gs.Params.InflationMin.String())
	assert.Equal(t, "0.670000000000000000", gs.Params.GoalBonded.String())
}
