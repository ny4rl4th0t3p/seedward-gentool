package rehearse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONScalar(t *testing.T) {
	assert.Equal(t, "100", jsonScalar([]byte(`100`)))     // proto-JSON number
	assert.Equal(t, "100", jsonScalar([]byte(`"100"`)))   // amino string
	assert.Equal(t, "0.05", jsonScalar([]byte(`"0.05"`))) // quoted decimal
	assert.Equal(t, "uatom", jsonScalar([]byte(`"uatom"`)))
	assert.Empty(t, jsonScalar([]byte(`null`)))
	assert.Empty(t, jsonScalar(nil))
}

func TestFloatEq(t *testing.T) {
	assert.True(t, floatEq("0.05", "0.050000000000000000"))
	assert.True(t, floatEq("0.0001", "0.000100000000000000"))
	assert.True(t, floatEq("1814400", "1814400"))
	assert.False(t, floatEq("0.05", "0.06"))
	assert.False(t, floatEq("0.05", "notanumber"))
}

func TestDurationSeconds(t *testing.T) {
	for in, want := range map[string]int64{
		"1814400s": 1814400,
		"504h0m0s": 1814400,
		"600s":     600,
		"172800s":  172800,
	} {
		got, err := durationSeconds(in)
		require.NoError(t, err)
		assert.Equal(t, want, got, in)
	}
	_, err := durationSeconds("notaduration")
	require.Error(t, err)
}

func TestCoinAmount(t *testing.T) {
	coins := []byte(`[{"denom":"uatom","amount":"500000"},{"denom":"other","amount":"1"}]`)
	assert.Equal(t, "500000", coinAmount(coins, "uatom"))
	assert.Empty(t, coinAmount(coins, "missing"))
	assert.Empty(t, coinAmount(nil, "uatom"))
}

func TestParamsMap(t *testing.T) {
	wrapped := paramsMap([]byte(`{"params":{"bond_denom":"uatom"}}`))
	assert.Equal(t, "uatom", jsonScalar(wrapped["bond_denom"]))
	flat := paramsMap([]byte(`{"bond_denom":"stake"}`))
	assert.Equal(t, "stake", jsonScalar(flat["bond_denom"]))
}

func TestUnwrapAccount_ProtoAndAmino(t *testing.T) {
	proto := []byte(`{"account":{"@type":"/cosmos.vesting.v1beta1.DelayedVestingAccount",` +
		`"base_vesting_account":{"original_vesting":[{"denom":"uatom","amount":"1000000"}],` +
		`"delegated_vesting":[{"denom":"uatom","amount":"900000"}]}}}`)
	inner, typ := unwrapAccount(proto)
	assert.Contains(t, typ, "DelayedVestingAccount")
	bva := baseVestingAccount(inner)
	assert.Equal(t, "1000000", coinAmount(bva["original_vesting"], "uatom"))
	assert.Equal(t, "900000", coinAmount(bva["delegated_vesting"], "uatom"))

	amino := []byte(`{"type":"cosmos-sdk/ContinuousVestingAccount","value":{` +
		`"base_vesting_account":{"original_vesting":[{"denom":"uatom","amount":"2000000"}]},` +
		`"start_time":"1735987170"}}`)
	innerA, typA := unwrapAccount(amino)
	assert.Contains(t, typA, "ContinuousVestingAccount")
	assert.Equal(t, "2000000", coinAmount(baseVestingAccount(innerA)["original_vesting"], "uatom"))
	assert.Equal(t, "1735987170", scalarFromSub(innerA["value"], "start_time"))
}

func TestTypeOf_ProtoAndAmino(t *testing.T) {
	assert.Contains(t, typeOf([]byte(`{"@type":"/cosmos.authz.v1beta1.GenericAuthorization"}`)), "GenericAuthorization")
	assert.Contains(t, typeOf([]byte(`{"type":"cosmos-sdk/GenericAuthorization"}`)), "GenericAuthorization")
}

func TestAllowanceSpendLimit_ProtoAndAmino(t *testing.T) {
	proto := []byte(`{"@type":"/cosmos.feegrant.v1beta1.BasicAllowance","spend_limit":[{"denom":"uatom","amount":"5000000"}]}`)
	assert.Equal(t, "5000000", allowanceSpendLimit(proto, "uatom"))
	amino := []byte(`{"type":"cosmos-sdk/BasicAllowance","value":{"spend_limit":[{"denom":"uatom","amount":"5000000"}]}}`)
	assert.Equal(t, "5000000", allowanceSpendLimit(amino, "uatom"))
}

func TestMaxExponent(t *testing.T) {
	units := []byte(`[{"denom":"uatom","exponent":0},{"denom":"atom","exponent":6}]`)
	assert.Equal(t, "6", maxExponent(units))
}

func TestCsvRows(t *testing.T) {
	rows := csvRows([]byte("addr1,100\naddr2,200,validator-alpha\n"))
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"addr1", "100"}, rows[0])
	assert.Equal(t, []string{"addr2", "200", "validator-alpha"}, rows[1])
	assert.Nil(t, csvRows(nil))
}

// eachRow must cap per-category work and disclose the truncation with a SKIP step.
func TestEachRow_CapsAndDiscloses(t *testing.T) {
	var b strings.Builder
	total := maxItemsPerCategory + 10
	for i := range total {
		fmt.Fprintf(&b, "addr%d,100\n", i)
	}
	a := &asserter{}
	calls := 0
	a.eachRow("accounts", []byte(b.String()), func(_ []string) { calls++ })

	assert.Equal(t, maxItemsPerCategory, calls)
	require.Len(t, a.steps, 1)
	assert.Equal(t, "assert:accounts_capped", a.steps[0].Name)
	assert.Equal(t, StepSkip, a.steps[0].Status)
}
