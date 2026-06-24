package genesis

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/encoding"
)

// Claims and grants are optional: a nil repository must be treated as "no records",
// not panic (mirroring the authz/feegrant repositories). The encoding config is unused
// on the nil path, so a zero value is fine.
func TestAccountsBuilder_NilClaimGrantRepos(t *testing.T) {
	va := accountsBuilder{cfg: ChainConfig{AddressPrefix: "cosmos", BondDenom: "uatom"}}
	var enc encoding.EncodingConfig

	claims, err := va.getClaims(context.Background(), enc)
	require.NoError(t, err)
	assert.Empty(t, claims)

	grants, err := va.getGrants(context.Background(), enc)
	require.NoError(t, err)
	assert.Empty(t, grants)
}

// fetchValidatorsShares reads claims; with no claim repository it must return empty
// shares rather than panic on a nil interface.
func TestFetchValidatorsShares_NilClaimRepo(t *testing.T) {
	va := accountsBuilder{cfg: ChainConfig{AddressPrefix: "cosmos", BondDenom: "uatom"}}
	var enc encoding.EncodingConfig

	shares, err := va.fetchValidatorsShares(enc)
	require.NoError(t, err)
	assert.Empty(t, shares)
}
