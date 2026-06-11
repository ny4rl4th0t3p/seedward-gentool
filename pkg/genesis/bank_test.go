package genesis

import (
	"encoding/json"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/encoding"
)

func newBankStateManager(t *testing.T, cfg ChainConfig) stateManager {
	t.Helper()
	ec := encoding.NewEncodingConfig()
	clientCtx := client.Context{}.WithCodec(ec.Codec)
	bankDefault := banktypes.DefaultGenesisState()
	bz, err := ec.Codec.MarshalJSON(bankDefault)
	require.NoError(t, err)
	return stateManager{
		encodingConfig: ec,
		clientCtx:      clientCtx,
		appGenState:    map[string]json.RawMessage{"bank": bz},
		cfg:            cfg,
	}
}

func TestSetDenominationMetadata_EmptyBase_NoOp(t *testing.T) {
	asm := newBankStateManager(t, ChainConfig{})
	original := make([]byte, len(asm.appGenState["bank"]))
	copy(original, asm.appGenState["bank"])

	require.NoError(t, asm.setDenominationMetadata())
	assert.Equal(t, string(original), string(asm.appGenState["bank"]))
}

func TestSetDenominationMetadata_BaseSet_MetadataWritten(t *testing.T) {
	asm := newBankStateManager(t, ChainConfig{
		DenomBase:        "uatom",
		DenomDisplay:     "atom",
		DenomSymbol:      "ATOM",
		DenomDescription: "The ATOM token",
		DenomExponent:    6,
	})
	require.NoError(t, asm.setDenominationMetadata())

	bankState := banktypes.GetGenesisStateFromAppState(asm.clientCtx.Codec, asm.appGenState)
	require.Len(t, bankState.DenomMetadata, 1)
	meta := bankState.DenomMetadata[0]
	assert.Equal(t, "uatom", meta.Base)
	assert.Equal(t, "atom", meta.Display)
	assert.Equal(t, "ATOM", meta.Symbol)
	assert.Equal(t, "The ATOM token", meta.Description)
	require.Len(t, meta.DenomUnits, 2)
	assert.Equal(t, "uatom", meta.DenomUnits[0].Denom)
	assert.Equal(t, uint32(0), meta.DenomUnits[0].Exponent)
	assert.Equal(t, "atom", meta.DenomUnits[1].Denom)
	assert.Equal(t, uint32(6), meta.DenomUnits[1].Exponent)
}

func TestSetDenominationMetadata_BaseEqualsDisplay_SingleDenomUnit(t *testing.T) {
	asm := newBankStateManager(t, ChainConfig{DenomBase: "uatom", DenomDisplay: "uatom"})
	require.NoError(t, asm.setDenominationMetadata())

	bankState := banktypes.GetGenesisStateFromAppState(asm.clientCtx.Codec, asm.appGenState)
	require.Len(t, bankState.DenomMetadata, 1)
	assert.Len(t, bankState.DenomMetadata[0].DenomUnits, 1)
}

func bankStateManagerWithSupply(t *testing.T, supply sdk.Coins, cfg ChainConfig) stateManager {
	t.Helper()
	ec := encoding.NewEncodingConfig()
	bankState := banktypes.DefaultGenesisState()
	bankState.Supply = supply
	bankBz, err := ec.Codec.MarshalJSON(bankState)
	require.NoError(t, err)
	return stateManager{
		clientCtx:   client.Context{}.WithCodec(ec.Codec),
		appGenState: map[string]json.RawMessage{"bank": bankBz},
		cfg:         cfg,
	}
}

func TestValidateSupply_Match_NoError(t *testing.T) {
	asm := bankStateManagerWithSupply(t,
		sdk.NewCoins(sdk.NewInt64Coin("uatom", 1_000_000)),
		ChainConfig{BondDenom: "uatom", TotalSupply: 1_000_000},
	)
	require.NoError(t, asm.validateSupply())
}

func TestValidateSupply_Mismatch_ReturnsError(t *testing.T) {
	asm := bankStateManagerWithSupply(t,
		sdk.NewCoins(sdk.NewInt64Coin("uatom", 1_000_000)),
		ChainConfig{BondDenom: "uatom", TotalSupply: 9_999_999},
	)
	err := asm.validateSupply()
	require.ErrorIs(t, err, ErrSupplyMismatch)
}
