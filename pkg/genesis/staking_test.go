package genesis

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/math"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/encoding"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/validator"
)

func TestApplyStakingParams_OnlyBondDenom_WhenNoConfigKeys(t *testing.T) {
	params := stakingtypes.DefaultParams()
	originalUnbonding := params.UnbondingTime
	originalMaxVal := params.MaxValidators

	applyStakingParams(&params, ChainConfig{BondDenom: "uatom"})

	assert.Equal(t, "uatom", params.BondDenom)
	assert.Equal(t, originalUnbonding, params.UnbondingTime)
	assert.Equal(t, originalMaxVal, params.MaxValidators)
}

func TestApplyStakingParams_AllConfigKeys(t *testing.T) {
	params := stakingtypes.DefaultParams()
	applyStakingParams(&params, ChainConfig{
		BondDenom:            "ustake",
		UnbondingTimeSeconds: 86400,
		MaxValidators:        150,
		MaxEntries:           5,
		HistoricalEntries:    5000,
		MinCommissionRate:    "0.05",
	})

	assert.Equal(t, "ustake", params.BondDenom)
	assert.Equal(t, 86400*time.Second, params.UnbondingTime)
	assert.Equal(t, uint32(150), params.MaxValidators)
	assert.Equal(t, uint32(5), params.MaxEntries)
	assert.Equal(t, uint32(5000), params.HistoricalEntries)
	assert.Equal(t, "0.050000000000000000", params.MinCommissionRate.String())
}

func TestApplyStakingParams_PartialKeys_OnlyUpdatesSet(t *testing.T) {
	params := stakingtypes.DefaultParams()
	originalEntries := params.MaxEntries
	originalUnbonding := params.UnbondingTime

	applyStakingParams(&params, ChainConfig{BondDenom: "uatom", MaxValidators: 200})

	assert.Equal(t, uint32(200), params.MaxValidators)
	assert.Equal(t, originalEntries, params.MaxEntries)
	assert.Equal(t, originalUnbonding, params.UnbondingTime)
}

// --- setStakingState ---

func stakingAppState(t *testing.T, ec encoding.EncodingConfig) map[string]json.RawMessage {
	t.Helper()
	gs := stakingtypes.DefaultGenesisState()
	bz, err := ec.Codec.MarshalJSON(gs)
	require.NoError(t, err)
	return map[string]json.RawMessage{"staking": bz}
}

func stakingTestConfig() ChainConfig {
	return ChainConfig{AddressPrefix: testHRP, BondDenom: "uatom", GenesisTime: 0}
}

func TestSetStakingState_ValidatorRepoError(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	sentinel := errors.New("repo fail")
	asm := stateManager{
		encodingConfig:      ec,
		validatorRepository: stubValidatorRepo{err: sentinel},
	}
	err := asm.setStakingState(stakingAppState(t, ec), nil, nil)
	require.ErrorIs(t, err, sentinel)
}

func TestSetStakingState_SingleValidator_InStakingState(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	v := testValidator(t, 1) // amount = 1_000_000
	asm := stateManager{
		encodingConfig:      ec,
		validatorRepository: stubValidatorRepo{validators: []validator.Validator{v}},
		cfg:                 stakingTestConfig(),
	}
	appGenState := stakingAppState(t, ec)
	require.NoError(t, asm.setStakingState(appGenState, nil, nil))

	// Unmarshal into a raw map to read the hand-crafted validator objects.
	var stakingRaw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenState["staking"], &stakingRaw))
	var vals []map[string]any
	require.NoError(t, json.Unmarshal(stakingRaw["validators"], &vals))

	require.Len(t, vals, 1)
	assert.Equal(t, v.OperatorAddress(), vals[0]["operator_address"])
	assert.Equal(t, "BOND_STATUS_BONDED", vals[0]["status"])
	assert.Equal(t, "1000000", vals[0]["tokens"])
}

func TestSetStakingState_SharesAddedToTokens(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	v := testValidator(t, 2) // amount = 1_000_000
	shares := map[string]int64{"validator-2": 3_000_000}
	asm := stateManager{
		encodingConfig:      ec,
		validatorRepository: stubValidatorRepo{validators: []validator.Validator{v}},
		cfg:                 stakingTestConfig(),
	}
	appGenState := stakingAppState(t, ec)
	require.NoError(t, asm.setStakingState(appGenState, nil, shares))

	var stakingRaw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenState["staking"], &stakingRaw))
	var vals []map[string]any
	require.NoError(t, json.Unmarshal(stakingRaw["validators"], &vals))

	require.Len(t, vals, 1)
	assert.Equal(t, "4000000", vals[0]["tokens"]) // 1M + 3M shares
}

func TestSetStakingState_DelegationsIncluded(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	v := testValidator(t, 3)
	existingDelegation := stakingtypes.Delegation{
		DelegatorAddress: testAccAddr(60).String(),
		ValidatorAddress: v.OperatorAddress(),
		Shares:           math.LegacyNewDec(500_000),
	}
	asm := stateManager{
		encodingConfig:      ec,
		validatorRepository: stubValidatorRepo{validators: []validator.Validator{v}},
		cfg:                 stakingTestConfig(),
	}
	appGenState := stakingAppState(t, ec)
	require.NoError(t, asm.setStakingState(appGenState, []stakingtypes.Delegation{existingDelegation}, nil))

	// Unmarshal via codec to read delegations.
	var stakingRaw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenState["staking"], &stakingRaw))
	var gs stakingtypes.GenesisState
	require.NoError(t, ec.Codec.UnmarshalJSON(appGenState["staking"], &gs))

	// existing delegation + validator self-delegation appended by setStakingState
	require.Len(t, gs.Delegations, 2)
}

func TestSetStakingState_GenutilCleared(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	asm := stateManager{
		encodingConfig:      ec,
		validatorRepository: stubValidatorRepo{},
		cfg:                 stakingTestConfig(),
	}
	appGenState := stakingAppState(t, ec)
	appGenState["genutil"] = json.RawMessage(`{"gen_txs":["original"]}`)
	require.NoError(t, asm.setStakingState(appGenState, nil, nil))

	assert.JSONEq(t, `{"gen_txs":[]}`, string(appGenState["genutil"]))
}
