package genesis

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/encoding"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/validator"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/vestingaccount"
)

// testValidator creates a deterministic Validator for test index i.
func testValidator(t *testing.T, i byte) validator.Validator {
	t.Helper()
	raw := make([]byte, 20)
	raw[19] = i
	opAddr, err := bech32.ConvertAndEncode(testHRP+"valoper", raw)
	require.NoError(t, err)
	pubKey := base64.StdEncoding.EncodeToString(append(make([]byte, 31), i))
	opPubKey := base64.StdEncoding.EncodeToString(make([]byte, 33))

	v, err := validator.NewValidatorFromFields(
		testHRP,
		opAddr, pubKey, "ed25519",
		fmt.Sprintf("validator-%d", i),
		"", "", "", "",
		"0.1", "0.2", "0.05", "1", "", "uatom", opPubKey,
		1_000_000,
	)
	require.NoError(t, err)
	return *v
}

func TestBuildValidatorReference_Empty(t *testing.T) {
	ref := buildValidatorReference(nil)
	assert.Empty(t, ref)
}

// --- fetchValidatorsShares ---

// stubClaimRepo is a minimal ClaimRepository for fetchValidatorsShares tests.
type stubClaimRepo struct {
	claims []vestingaccount.Claim
	err    error
}

func (s stubClaimRepo) GetClaims(_ context.Context, _ encoding.EncodingConfig) ([]vestingaccount.Claim, error) {
	return s.claims, s.err
}

// makeClaim creates a Claim value using the test HRP address for index i.
func makeClaim(t *testing.T, ec encoding.EncodingConfig, addrIdx byte, amount int64, delegateTo string) vestingaccount.Claim {
	t.Helper()
	addr := testAccAddr(addrIdx).String()
	c, err := vestingaccount.NewClaim(addr, amount, delegateTo, ec)
	require.NoError(t, err)
	return *c
}

func TestFetchValidatorsShares_Empty(t *testing.T) {
	acc := Accounts{claimRepository: stubClaimRepo{}}
	shares, err := acc.fetchValidatorsShares(encoding.NewEncodingConfig())
	require.NoError(t, err)
	assert.Empty(t, shares)
}

func TestFetchValidatorsShares_NoDelegateTo_EmptyShares(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	claims := []vestingaccount.Claim{
		makeClaim(t, ec, 70, 1_000_000, ""),
	}
	acc := Accounts{claimRepository: stubClaimRepo{claims: claims}}
	shares, err := acc.fetchValidatorsShares(ec)
	require.NoError(t, err)
	assert.Empty(t, shares)
}

func TestFetchValidatorsShares_Accumulates(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	v1 := testValidator(t, 1)
	claims := []vestingaccount.Claim{
		makeClaim(t, ec, 70, 1_500_000, v1.OperatorAddress()),
		makeClaim(t, ec, 71, 2_000_000, v1.OperatorAddress()),
	}
	acc := Accounts{claimRepository: stubClaimRepo{claims: claims}}
	shares, err := acc.fetchValidatorsShares(ec)
	require.NoError(t, err)
	// delta1 = 1_500_000 - 100_000 = 1_400_000
	// delta2 = 2_000_000 - 100_000 = 1_900_000
	assert.Equal(t, int64(3_300_000), shares[v1.OperatorAddress()])
}

func TestFetchValidatorsShares_Overflow_ReturnsError(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	v1 := testValidator(t, 1)
	// After claim1: shares[v] = math.MaxInt64 - 100_000
	// claim2: delta2 = 200_001 - 100_000 = 100_001
	// Check: shares[v] > MaxInt64 - delta2 → (MaxInt64-100_000) > (MaxInt64-100_001) → true
	claims := []vestingaccount.Claim{
		makeClaim(t, ec, 72, math.MaxInt64, v1.OperatorAddress()),
		makeClaim(t, ec, 73, 200_001, v1.OperatorAddress()),
	}
	acc := Accounts{claimRepository: stubClaimRepo{claims: claims}}
	_, err := acc.fetchValidatorsShares(ec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overflow")
}

func TestFetchValidatorsShares_AmountAtOrBelowReserve_ReturnsError(t *testing.T) {
	ec := encoding.NewEncodingConfig()
	v1 := testValidator(t, 1)
	// amount == default reserve (100_000) → no positive stake possible.
	claims := []vestingaccount.Claim{
		makeClaim(t, ec, 74, 100_000, v1.OperatorAddress()),
	}
	acc := Accounts{claimRepository: stubClaimRepo{claims: claims}}
	_, err := acc.fetchValidatorsShares(ec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must exceed")
}

func TestBuildValidatorReference_Normal(t *testing.T) {
	v1 := testValidator(t, 1)
	v2 := testValidator(t, 2)

	ref := buildValidatorReference([]validator.Validator{v1, v2})

	require.Len(t, ref, 2)
	assert.Equal(t, v1.OperatorAddress(), ref[v1.Name()].OperatorAddress)
	assert.Equal(t, v1.DelegatorAddress(), ref[v1.Name()].DelegatorAddress)
	assert.Equal(t, v2.OperatorAddress(), ref[v2.Name()].OperatorAddress)
	assert.Equal(t, v2.DelegatorAddress(), ref[v2.Name()].DelegatorAddress)
}
