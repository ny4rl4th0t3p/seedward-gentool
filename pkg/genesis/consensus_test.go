package genesis

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/validator"
)

// stubValidatorRepo is a minimal ValidatorRepository for consensus tests.
type stubValidatorRepo struct {
	validators []validator.Validator
	err        error
}

func (s stubValidatorRepo) GetValidators(_ context.Context) ([]validator.Validator, error) {
	return s.validators, s.err
}

func newConsensusForTest(t *testing.T, validators []validator.Validator, shares map[string]int64) (*consensus, *genutiltypes.AppGenesis) {
	t.Helper()
	appGenesis := &genutiltypes.AppGenesis{}
	c := newConsensus(
		stubValidatorRepo{validators: validators},
		appGenesis,
		nil, // codec field is not used by setParams
		shares,
	)
	return c, appGenesis
}

func TestSetParams_EmptyValidators_SetsConsensusWithNoValidators(t *testing.T) {
	c, appGenesis := newConsensusForTest(t, nil, nil)
	require.NoError(t, c.setParams())
	require.NotNil(t, appGenesis.Consensus)
	assert.Empty(t, appGenesis.Consensus.Validators)
}

func TestSetParams_SingleValidator_PowerAndFields(t *testing.T) {
	v := testValidator(t, 1) // amount = 1_000_000
	c, appGenesis := newConsensusForTest(t, []validator.Validator{v}, nil)
	require.NoError(t, c.setParams())

	require.Len(t, appGenesis.Consensus.Validators, 1)
	gv := appGenesis.Consensus.Validators[0]
	assert.Equal(t, "validator-1", gv.Name)
	assert.Equal(t, int64(1), gv.Power) // 1_000_000 / 1_000_000
	assert.Equal(t, v.ConsensusAddress(), []byte(gv.Address))
}

func TestSetParams_PowerIncludesShares(t *testing.T) {
	v := testValidator(t, 2) // amount = 1_000_000
	shares := map[string]int64{"validator-2": 4_000_000}
	c, appGenesis := newConsensusForTest(t, []validator.Validator{v}, shares)
	require.NoError(t, c.setParams())

	require.Len(t, appGenesis.Consensus.Validators, 1)
	assert.Equal(t, int64(5), appGenesis.Consensus.Validators[0].Power) // (1_000_000 + 4_000_000) / 1_000_000
}

func TestSetParams_MultipleValidators_AllIncluded(t *testing.T) {
	v1 := testValidator(t, 3)
	v2 := testValidator(t, 4)
	c, appGenesis := newConsensusForTest(t, []validator.Validator{v1, v2}, nil)
	require.NoError(t, c.setParams())

	assert.Len(t, appGenesis.Consensus.Validators, 2)
}

func TestSetParams_ConsensusParamDefaults(t *testing.T) {
	c, appGenesis := newConsensusForTest(t, nil, nil)
	require.NoError(t, c.setParams())

	params := appGenesis.Consensus.Params
	require.NotNil(t, params)
	assert.Equal(t, int64(defaultBlockMaxBytes), params.Block.MaxBytes)
	assert.Equal(t, int64(defaultBlockMaxGas), params.Block.MaxGas)
	assert.Equal(t, int64(defaultEvidenceMaxAgeNumBlocks), params.Evidence.MaxAgeNumBlocks)
	assert.Equal(t, defaultEvidenceMaxAgeDuration, params.Evidence.MaxAgeDuration)
	assert.Equal(t, int64(defaultEvidenceMaxBytes), params.Evidence.MaxBytes)
	assert.Equal(t, []string{"ed25519"}, params.Validator.PubKeyTypes)
}

func TestSetParams_InvalidPubKeyLength_ReturnsError(t *testing.T) {
	// 16-byte pubkey passes validator construction (SHA256 accepts any length),
	// but setParams rejects it because ed25519 requires exactly 32 bytes.
	shortPubKey := base64.StdEncoding.EncodeToString(make([]byte, 16))
	raw := make([]byte, 20)
	raw[19] = 10
	opAddr, err := bech32.ConvertAndEncode(testHRP+"valoper", raw)
	require.NoError(t, err)
	opPubKey := base64.StdEncoding.EncodeToString(make([]byte, 33))

	v, err := validator.NewValidatorFromFields(
		testHRP,
		opAddr, shortPubKey, "ed25519", "bad-pubkey-val",
		"", "", "", "",
		"0.1", "0.2", "0.05", "1", "", "uatom", opPubKey,
		1_000_000,
	)
	require.NoError(t, err)

	c, _ := newConsensusForTest(t, []validator.Validator{*v}, nil)
	err = c.setParams()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pubkey length")
}
