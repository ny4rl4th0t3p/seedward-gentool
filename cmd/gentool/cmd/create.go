package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/csv"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/encoding"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/gentx"
)

const flagInputGenesis = "input-genesis"

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a genesis file from a baseline genesis and CSV inputs",
	RunE: func(cmd *cobra.Command, _ []string) error {
		inputGenesis, err := cmd.Flags().GetString(flagInputGenesis)
		if err != nil || inputGenesis == "" {
			return fmt.Errorf("--input-genesis is required: path to a baseline genesis file from '<chaind> init'")
		}

		hrp := viper.GetString("chain.address_prefix")
		if hrp == "" {
			return fmt.Errorf("chain.address_prefix is required in config")
		}
		cfg := buildChainConfig(hrp)

		raw, err := os.ReadFile(inputGenesis)
		if err != nil {
			return fmt.Errorf("failed to read --input-genesis %s: %w", inputGenesis, err)
		}

		moduleAddresses := computeAllModuleAddresses(hrp, cfg.ExtraModules)

		repos := genesis.Repositories{
			Claims:          csv.NewCSVClaimRepository(viper.GetString("claims.file_name"), moduleAddresses),
			Grants:          csv.NewCSVGrantRepository(viper.GetString("grants.file_name"), moduleAddresses),
			InitialAccounts: csv.NewCSVInitialAccountsRepository(viper.GetString("accounts.file_name"), moduleAddresses),
			Validators:      gentx.NewValidatorRepository(viper.GetString("validators.gentx_dir"), hrp),
		}
		// authz/feegrant are optional: a nil repository signals "module not configured".
		if viper.IsSet("authz.file_name") {
			repos.AuthzGrants = csv.NewCSVAuthzGrantRepository(viper.GetString("authz.file_name"), moduleAddresses)
		}
		if viper.IsSet("feegrant.file_name") {
			repos.FeeAllowances = csv.NewCSVFeeAllowanceRepository(viper.GetString("feegrant.file_name"), moduleAddresses)
		}

		appGenesis, err := genesis.Build(context.Background(), raw, cfg, repos)
		if err != nil {
			slog.Error(err.Error())
			return err
		}

		return appGenesis.SaveAs(viper.GetString("genesis.output"))
	},
}

// buildChainConfig assembles the genesis.ChainConfig from viper. This is the single
// place viper is read for genesis construction; the genesis package takes the struct.
func buildChainConfig(hrp string) genesis.ChainConfig {
	type extraModuleConfig struct {
		Name        string   `mapstructure:"name"`
		Permissions []string `mapstructure:"permissions"`
	}
	var raw []extraModuleConfig
	_ = viper.UnmarshalKey("modules.extra", &raw)
	extra := make([]genesis.ExtraModule, 0, len(raw))
	for _, em := range raw {
		extra = append(extra, genesis.ExtraModule{Name: em.Name, Permissions: em.Permissions})
	}

	return genesis.ChainConfig{
		ChainID:       viper.GetString("chain.id"),
		AppName:       viper.GetString("app.name"),
		AppVersion:    viper.GetString("app.version"),
		GenesisTime:   viper.GetInt64("app.genesis_time"),
		InitialHeight: viper.GetInt64("chain.initial_height"),

		AddressPrefix: hrp,
		BondDenom:     viper.GetString("default_bond_denom"),

		TotalSupply:     viper.GetInt64("accounts.total_supply"),
		NonStakedAmount: viper.GetInt64("accounts.non_staked_amount"),

		ClaimsVestingEnd:   viper.GetInt64("claims.vesting.end_date"),
		GrantsVestingStart: viper.GetInt64("grants.vesting.start_date"),
		GrantsVestingEnd:   viper.GetInt64("grants.vesting.end_date"),

		DenomBase:        viper.GetString("denom.base"),
		DenomDisplay:     viper.GetString("denom.display"),
		DenomSymbol:      viper.GetString("denom.symbol"),
		DenomDescription: viper.GetString("denom.description"),
		DenomExponent:    viper.GetUint32("denom.exponent"),
		DenomAliases:     viper.GetStringSlice("denom.aliases"),

		ExtraModules: extra,

		UnbondingTimeSeconds: viper.GetInt64("chain.unbonding_time_seconds"),
		MaxValidators:        viper.GetUint32("chain.max_validators"),
		MaxEntries:           viper.GetUint32("chain.max_entries"),
		HistoricalEntries:    viper.GetUint32("chain.historical_entries"),
		MinCommissionRate:    viper.GetString("chain.min_commission_rate"),

		BlocksPerYear:       viper.GetInt64("chain.blocks_per_year"),
		InflationRateChange: viper.GetString("chain.inflation_rate_change"),
		InflationMax:        viper.GetString("chain.inflation_max"),
		InflationMin:        viper.GetString("chain.inflation_min"),
		GoalBonded:          viper.GetString("chain.goal_bonded"),

		GovMinDepositAmount:          viper.GetInt64("gov.min_deposit_amount"),
		GovVotingPeriod:              viper.GetString("gov.voting_period"),
		GovExpeditedMinDepositAmount: viper.GetInt64("gov.expedited_min_deposit_amount"),
		GovExpeditedVotingPeriod:     viper.GetString("gov.expedited_voting_period"),

		SignedBlocksWindow:          viper.GetInt64("slashing.signed_blocks_window"),
		MinSignedPerWindow:          viper.GetString("slashing.min_signed_per_window"),
		DowntimeJailDurationSeconds: viper.GetInt64("slashing.downtime_jail_duration_seconds"),
		SlashFractionDoubleSign:     viper.GetString("slashing.slash_fraction_double_sign"),
		SlashFractionDowntime:       viper.GetString("slashing.slash_fraction_downtime"),

		CommunityPoolAmount: viper.GetInt64("distribution.community_pool_amount"),
	}
}

func computeAllModuleAddresses(hrp string, extraModules []genesis.ExtraModule) map[string]bool {
	names := make([]string, 0, len(encoding.StandardModuleNames)+len(extraModules))
	names = append(names, encoding.StandardModuleNames...)
	for _, em := range extraModules {
		names = append(names, em.Name)
	}
	return encoding.ModuleAddresses(hrp, names)
}

func init() {
	rootCmd.AddCommand(createCmd)
	createCmd.Flags().String(flagInputGenesis, "", "Path to baseline genesis file generated by '<chaind> init' (required)")
}
