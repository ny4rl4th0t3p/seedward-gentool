package app

// ExtraModule is a chain-specific module account (e.g. Nillion's "meta" module)
// that is not part of the standard Cosmos SDK module set.
type ExtraModule struct {
	Name        string
	Permissions []string
}

// ChainConfig holds every configuration value the genesis-construction logic
// needs. It is built once from viper in cmd/gentool and passed into internal/app
// so the package itself never reads global config. Zero/empty fields mean
// "keep the baseline genesis default" unless documented otherwise.
type ChainConfig struct {
	// Genesis metadata
	ChainID       string
	AppName       string
	AppVersion    string
	GenesisTime   int64 // unix timestamp
	InitialHeight int64

	// Chain identity
	AddressPrefix string
	BondDenom     string

	// Supply
	TotalSupply int64
	// NonStakedAmount is the absolute base-denom liquid reserve left un-delegated
	// on each delegating account, so it retains a spendable balance to pay gas
	// (undelegate, vote, withdraw rewards). It is a fixed amount, NOT a percentage.
	// 0 or unset → defaultNonStakedAmount. A delegating claim's amount must exceed it.
	NonStakedAmount int64

	// Vesting windows (unix timestamps; 0 means unset)
	ClaimsVestingEnd   int64
	GrantsVestingStart int64
	GrantsVestingEnd   int64

	// Denom metadata (DenomBase empty → no metadata written)
	DenomBase        string
	DenomDisplay     string
	DenomSymbol      string
	DenomDescription string
	DenomExponent    uint32
	DenomAliases     []string

	// Extra module accounts (chain-specific)
	ExtraModules []ExtraModule

	// Staking (0/empty → keep genesis default)
	UnbondingTimeSeconds int64
	MaxValidators        uint32
	MaxEntries           uint32
	HistoricalEntries    uint32
	MinCommissionRate    string

	// Mint (0/empty → keep genesis default)
	BlocksPerYear       int64
	InflationRateChange string
	InflationMax        string
	InflationMin        string
	GoalBonded          string

	// Gov (0/empty → keep genesis default)
	GovMinDepositAmount          int64
	GovVotingPeriod              string
	GovExpeditedMinDepositAmount int64
	GovExpeditedVotingPeriod     string

	// Slashing (0/empty → keep genesis default)
	SignedBlocksWindow          int64
	MinSignedPerWindow          string
	DowntimeJailDurationSeconds int64
	SlashFractionDoubleSign     string
	SlashFractionDowntime       string

	// Distribution
	CommunityPoolAmount int64
}

// NonStaked returns the effective liquid reserve, falling back to the default when unset.
func (c ChainConfig) NonStaked() int64 {
	if c.NonStakedAmount > 0 {
		return c.NonStakedAmount
	}
	return defaultNonStakedAmount
}
