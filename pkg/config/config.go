// Package config loads a gentool YAML configuration into the genesis-construction inputs: the
// chain config plus the file paths it names. It depends only on viper and the genesis package
// (no cobra/CLI machinery), so non-CLI consumers — e.g. the rehearsal runner — can reuse the
// exact schema mapping the gentool command uses, without pulling in the command tree.
package config

import (
	"fmt"

	"github.com/spf13/viper"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
)

// Inputs are the genesis-construction inputs named by a gentool config file: the chain config
// plus the on-disk locations of each allocation CSV and the gentx directory. An empty path
// field means the config did not set that (optional) input.
type Inputs struct {
	Chain    genesis.ChainConfig
	GentxDir string
	Accounts string
	Claims   string
	Grants   string
	Authz    string
	Feegrant string
}

// Load parses a gentool config file (the same file `gentool create` consumes) into its
// genesis-construction Inputs. It requires an explicit path and errors if the file cannot be
// read — the entry point for external callers such as the rehearsal runner.
func Load(cfgFile string) (Inputs, error) {
	if cfgFile == "" {
		return Inputs{}, fmt.Errorf("config file path is required")
	}
	v := viper.New()
	v.SetConfigFile(cfgFile)
	v.AutomaticEnv()
	if err := v.ReadInConfig(); err != nil {
		return Inputs{}, fmt.Errorf("read config %s: %w", cfgFile, err)
	}
	return FromViper(v)
}

// FromViper maps an already-loaded viper into Inputs. It is the single viper→Inputs path,
// shared by Load (external callers) and the create command, so the two never drift.
func FromViper(v *viper.Viper) (Inputs, error) {
	hrp := v.GetString("chain.address_prefix")
	if hrp == "" {
		return Inputs{}, fmt.Errorf("chain.address_prefix is required in config")
	}
	return Inputs{
		Chain:    buildChainConfig(v, hrp),
		GentxDir: v.GetString("validators.gentx_dir"),
		Accounts: v.GetString("accounts.file_name"),
		Claims:   v.GetString("claims.file_name"),
		Grants:   v.GetString("grants.file_name"),
		Authz:    v.GetString("authz.file_name"),
		Feegrant: v.GetString("feegrant.file_name"),
	}, nil
}

// buildChainConfig assembles the genesis.ChainConfig from viper. This is the single place
// viper is read for genesis construction; the genesis package takes the struct.
func buildChainConfig(v *viper.Viper, hrp string) genesis.ChainConfig {
	type extraModuleConfig struct {
		Name        string   `mapstructure:"name"`
		Permissions []string `mapstructure:"permissions"`
	}
	var raw []extraModuleConfig
	_ = v.UnmarshalKey("modules.extra", &raw)
	extra := make([]genesis.ExtraModule, 0, len(raw))
	for _, em := range raw {
		extra = append(extra, genesis.ExtraModule{Name: em.Name, Permissions: em.Permissions})
	}

	return genesis.ChainConfig{
		ChainID:       v.GetString("chain.id"),
		AppName:       v.GetString("app.name"),
		AppVersion:    v.GetString("app.version"),
		GenesisTime:   v.GetInt64("app.genesis_time"),
		InitialHeight: v.GetInt64("chain.initial_height"),

		AddressPrefix: hrp,
		BondDenom:     v.GetString("default_bond_denom"),

		TotalSupply:     v.GetInt64("accounts.total_supply"),
		NonStakedAmount: v.GetInt64("accounts.non_staked_amount"),

		ClaimsVestingEnd:   v.GetInt64("claims.vesting.end_date"),
		GrantsVestingStart: v.GetInt64("grants.vesting.start_date"),
		GrantsVestingEnd:   v.GetInt64("grants.vesting.end_date"),

		DenomBase:        v.GetString("denom.base"),
		DenomDisplay:     v.GetString("denom.display"),
		DenomSymbol:      v.GetString("denom.symbol"),
		DenomDescription: v.GetString("denom.description"),
		DenomExponent:    v.GetUint32("denom.exponent"),
		DenomAliases:     v.GetStringSlice("denom.aliases"),

		ExtraModules: extra,

		UnbondingTimeSeconds: v.GetInt64("chain.unbonding_time_seconds"),
		MaxValidators:        v.GetUint32("chain.max_validators"),
		MaxEntries:           v.GetUint32("chain.max_entries"),
		HistoricalEntries:    v.GetUint32("chain.historical_entries"),
		MinCommissionRate:    v.GetString("chain.min_commission_rate"),

		BlocksPerYear:       v.GetInt64("chain.blocks_per_year"),
		InflationRateChange: v.GetString("chain.inflation_rate_change"),
		InflationMax:        v.GetString("chain.inflation_max"),
		InflationMin:        v.GetString("chain.inflation_min"),
		GoalBonded:          v.GetString("chain.goal_bonded"),

		GovMinDepositAmount:          v.GetInt64("gov.min_deposit_amount"),
		GovVotingPeriod:              v.GetString("gov.voting_period"),
		GovExpeditedMinDepositAmount: v.GetInt64("gov.expedited_min_deposit_amount"),
		GovExpeditedVotingPeriod:     v.GetString("gov.expedited_voting_period"),

		SignedBlocksWindow:          v.GetInt64("slashing.signed_blocks_window"),
		MinSignedPerWindow:          v.GetString("slashing.min_signed_per_window"),
		DowntimeJailDurationSeconds: v.GetInt64("slashing.downtime_jail_duration_seconds"),
		SlashFractionDoubleSign:     v.GetString("slashing.slash_fraction_double_sign"),
		SlashFractionDowntime:       v.GetString("slashing.slash_fraction_downtime"),

		CommunityPoolAmount: v.GetInt64("distribution.community_pool_amount"),
	}
}
