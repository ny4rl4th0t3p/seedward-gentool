package genesis

import (
	"context"
	"fmt"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// Repositories bundles all data sources required to construct genesis.
// CSV implementations are provided in pkg/genesis/csv (gentx for validators).
//
// InitialAccounts and Validators are required. Claims, Grants, AuthzGrants, and
// FeeAllowances are optional: a nil repository is treated as "no records" for that
// module, never a panic.
type Repositories struct {
	InitialAccounts InitialAccountsRepository // required (≥1 account)
	Validators      ValidatorRepository       // required
	Claims          ClaimRepository           // nil → no claims
	Grants          GrantRepository           // nil → no grants
	AuthzGrants     AuthzGrantRepository      // nil → skip authz genesis
	FeeAllowances   FeeAllowanceRepository    // nil → skip feegrant genesis
}

// sdkConfigOnce guards the process-global sdk.Config seal so Build can be called
// repeatedly (and alongside test setup) without panicking on a double-seal.
var sdkConfigOnce sync.Once

// sealSDKConfig sets the global SDK bech32 prefixes from the address prefix and
// seals the config. Safe to call multiple times; only the first call takes effect.
func sealSDKConfig(addressPrefix string) {
	sdkConfigOnce.Do(func() {
		c := sdk.GetConfig()
		c.SetBech32PrefixForAccount(addressPrefix, addressPrefix+"pub")
		c.SetBech32PrefixForValidator(addressPrefix+"valoper", addressPrefix+"valoperpub")
		c.SetBech32PrefixForConsensusNode(addressPrefix+"valcons", addressPrefix+"valconspub")
		c.Seal()
	})
}

// Build constructs a genesis document from a baseline genesis (raw JSON from
// '<chaind> init') and the provided chain config and data sources. The returned
// AppGenesis is fully populated in memory; the caller saves it to disk.
//
// cfg.AddressPrefix and cfg.BondDenom must be set. The SDK global config
// (sdk.GetConfig bech32 prefixes + sdk.DefaultBondDenom) is configured inside
// Build; the bech32 seal happens once per process.
func Build(ctx context.Context, baseGenesis []byte, cfg ChainConfig, repos Repositories) (*genutiltypes.AppGenesis, error) {
	if cfg.AddressPrefix == "" {
		return nil, fmt.Errorf("cfg.AddressPrefix is required")
	}
	if cfg.BondDenom == "" {
		return nil, fmt.Errorf("cfg.BondDenom is required")
	}
	sealSDKConfig(cfg.AddressPrefix)
	sdk.DefaultBondDenom = cfg.BondDenom

	encodingConfig, clientCtx, appState, appGenesis, err := parseBaseGenesis(baseGenesis, cfg)
	if err != nil {
		return nil, err
	}

	asm := newAppStateManager(
		cfg,
		repos.Claims,
		repos.Grants,
		repos.InitialAccounts,
		repos.Validators,
		repos.AuthzGrants,
		repos.FeeAllowances,
		appState,
		appGenesis,
		encodingConfig,
		clientCtx,
	)

	finalGenesis, shares, err := asm.setupAppState(ctx)
	if err != nil {
		return nil, err
	}

	consensus := newConsensus(repos.Validators, finalGenesis, encodingConfig.TxConfig.SigningContext().AddressCodec(), shares)
	if err := consensus.setParams(); err != nil {
		return nil, err
	}

	return finalGenesis, nil
}
