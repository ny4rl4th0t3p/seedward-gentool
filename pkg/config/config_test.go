package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fullConfig = `
chain:
  id: test-1
  address_prefix: cosmos
  initial_height: 1
  max_validators: 100
  max_entries: 7
  historical_entries: 10000
  unbonding_time_seconds: 1814400
  min_commission_rate: "0.05"
  blocks_per_year: 6311520

default_bond_denom: uatom

app:
  name: gaia
  version: "0.0.1"
  genesis_time: 1735987170

slashing:
  signed_blocks_window: 10000
  min_signed_per_window: "0.5"
  downtime_jail_duration_seconds: 600
  slash_fraction_double_sign: "0.05"
  slash_fraction_downtime: "0.0001"

gov:
  min_deposit_amount: 10000000
  voting_period: "172800s"

denom:
  base: uatom
  display: ATOM
  symbol: ATOM
  description: "the token"
  exponent: 6
  aliases: [microatom]

distribution:
  community_pool_amount: 500000

accounts:
  total_supply: 7000000
  non_staked_amount: 100000
  file_name: /data/accounts.csv

claims:
  file_name: /data/claims.csv
  vesting:
    end_date: 1900000000

grants:
  file_name: /data/grants.csv
  vesting:
    start_date: 1735987170
    end_date: 1900000000

authz:
  file_name: /data/authz.csv

feegrant:
  file_name: /data/feegrant.csv

validators:
  gentx_dir: /data/gentx

modules:
  extra:
    - name: meta
      permissions: [minter]
`

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gentool.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoad_MapsEverySection(t *testing.T) {
	in, err := Load(writeConfig(t, fullConfig))
	require.NoError(t, err)

	// paths
	assert.Equal(t, "/data/gentx", in.GentxDir)
	assert.Equal(t, "/data/accounts.csv", in.Accounts)
	assert.Equal(t, "/data/claims.csv", in.Claims)
	assert.Equal(t, "/data/grants.csv", in.Grants)
	assert.Equal(t, "/data/authz.csv", in.Authz)
	assert.Equal(t, "/data/feegrant.csv", in.Feegrant)

	c := in.Chain
	// identity + supply
	assert.Equal(t, "test-1", c.ChainID)
	assert.Equal(t, "cosmos", c.AddressPrefix)
	assert.Equal(t, "uatom", c.BondDenom)
	assert.Equal(t, int64(7000000), c.TotalSupply)
	assert.Equal(t, int64(100000), c.NonStakedAmount)
	assert.Equal(t, int64(1735987170), c.GenesisTime)
	// vesting windows
	assert.Equal(t, int64(1900000000), c.ClaimsVestingEnd)
	assert.Equal(t, int64(1735987170), c.GrantsVestingStart)
	assert.Equal(t, int64(1900000000), c.GrantsVestingEnd)
	// denom metadata
	assert.Equal(t, "uatom", c.DenomBase)
	assert.Equal(t, "ATOM", c.DenomDisplay)
	assert.Equal(t, uint32(6), c.DenomExponent)
	assert.Equal(t, []string{"microatom"}, c.DenomAliases)
	// staking / mint
	assert.Equal(t, int64(1814400), c.UnbondingTimeSeconds)
	assert.Equal(t, uint32(100), c.MaxValidators)
	assert.Equal(t, "0.05", c.MinCommissionRate)
	assert.Equal(t, int64(6311520), c.BlocksPerYear)
	// slashing / gov / distribution
	assert.Equal(t, int64(10000), c.SignedBlocksWindow)
	assert.Equal(t, "0.0001", c.SlashFractionDowntime)
	assert.Equal(t, int64(10000000), c.GovMinDepositAmount)
	assert.Equal(t, "172800s", c.GovVotingPeriod)
	assert.Equal(t, int64(500000), c.CommunityPoolAmount)
	// extra modules
	require.Len(t, c.ExtraModules, 1)
	assert.Equal(t, "meta", c.ExtraModules[0].Name)
	assert.Equal(t, []string{"minter"}, c.ExtraModules[0].Permissions)
}

func TestLoad_OptionalPathsAbsent(t *testing.T) {
	const minimal = `
chain:
  address_prefix: cosmos
default_bond_denom: uatom
accounts:
  total_supply: 1000
  file_name: /data/accounts.csv
validators:
  gentx_dir: /data/gentx
`
	in, err := Load(writeConfig(t, minimal))
	require.NoError(t, err)
	assert.Equal(t, "/data/accounts.csv", in.Accounts)
	assert.Empty(t, in.Claims)
	assert.Empty(t, in.Grants)
	assert.Empty(t, in.Authz)
	assert.Empty(t, in.Feegrant)
}

func TestLoad_Errors(t *testing.T) {
	_, err := Load("")
	require.Error(t, err, "empty path")

	_, err = Load(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err, "missing file")

	// present file but no address prefix
	path := writeConfig(t, "default_bond_denom: uatom\n")
	_, err = Load(path)
	require.Error(t, err, "missing chain.address_prefix")
}

func TestFromViper_RequiresAddressPrefix(t *testing.T) {
	v := viper.New()
	v.Set("default_bond_denom", "uatom")
	_, err := FromViper(v)
	require.Error(t, err)

	v.Set("chain.address_prefix", "cosmos")
	in, err := FromViper(v)
	require.NoError(t, err)
	assert.Equal(t, "cosmos", in.Chain.AddressPrefix)
	assert.Equal(t, "uatom", in.Chain.BondDenom)
}
