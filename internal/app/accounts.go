package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	stdmath "math"
	"sort"
	"strconv"
	"strings"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/internal/repository"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/encoding"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/pkg/genesis/validator"
)

// defaultNonStakedAmount is the fallback liquid reserve (base denom) kept on each
// delegating account when accounts.non_staked_amount is unset. See ChainConfig.NonStakedAmount.
const defaultNonStakedAmount = 100_000

type Accounts struct {
	cfg                 ChainConfig
	claimRepository     repository.ClaimRepository
	grantRepository     repository.GrantRepository
	initialAccountsRepo repository.InitialAccountsRepository
	validatorRepository repository.ValidatorRepository
}

func NewAccounts(
	cfg ChainConfig,
	claimRepository repository.ClaimRepository,
	grantRepository repository.GrantRepository,
	initialAccountsRepo repository.InitialAccountsRepository,
	validatorRepository repository.ValidatorRepository,
) *Accounts {
	return &Accounts{
		cfg:                 cfg,
		claimRepository:     claimRepository,
		grantRepository:     grantRepository,
		initialAccountsRepo: initialAccountsRepo,
		validatorRepository: validatorRepository,
	}
}

func (va Accounts) fetchValidatorsShares(encodingConfig encoding.EncodingConfig) (map[string]int64, error) {
	shares := map[string]int64{}
	claims, err := va.claimRepository.GetClaims(context.Background(), encodingConfig)
	if err != nil {
		return nil, err
	}
	reserve := va.cfg.NonStaked()
	for _, claim := range claims {
		if claim.DelegateTo() != "" {
			if claim.Amount() <= reserve {
				return nil, fmt.Errorf(
					"claim %s delegating to %s: amount %d must exceed the non_staked_amount reserve %d",
					claim.Address(), claim.DelegateTo(), claim.Amount(), reserve,
				)
			}
			delta := claim.Amount() - reserve
			if shares[claim.DelegateTo()] > stdmath.MaxInt64-delta {
				return nil, fmt.Errorf("share accumulation overflow for validator %q", claim.DelegateTo())
			}
			shares[claim.DelegateTo()] += delta
		}
	}
	return shares, nil
}

// loadAuthBankState pulls the auth and bank genesis state out of the in-memory
// app state map and unpacks the accounts for mutation.
func loadAuthBankState(
	clientCtx client.Context, appState map[string]json.RawMessage,
) (
	authGenState authtypes.GenesisState,
	bankGenState *banktypes.GenesisState,
	accs authtypes.GenesisAccounts,
	err error,
) {
	authGenState = authtypes.GetGenesisStateFromAppState(clientCtx.Codec, appState)
	bankGenState = banktypes.GetGenesisStateFromAppState(clientCtx.Codec, appState)
	accs, err = authtypes.UnpackAccounts(authGenState.Accounts)
	if err != nil {
		return authtypes.GenesisState{}, nil, nil, fmt.Errorf("failed to extract accounts: %w", err)
	}
	return authGenState, bankGenState, accs, nil
}

// sealAuthBankState sanitizes and marshals the mutated auth and bank state back
// into the in-memory app state map. No disk I/O; the final genesis save handles that.
func sealAuthBankState(
	clientCtx client.Context,
	accs authtypes.GenesisAccounts,
	authGenState authtypes.GenesisState,
	bankGenState *banktypes.GenesisState,
	appState map[string]json.RawMessage,
) error {
	accs = authtypes.SanitizeGenesisAccounts(accs)
	packedAccounts, err := authtypes.PackAccounts(accs)
	if err != nil {
		return fmt.Errorf("failed to pack accounts: %w", err)
	}
	authGenState.Accounts = packedAccounts
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

	authGenStateBz, err := clientCtx.Codec.MarshalJSON(&authGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal auth genesis state: %w", err)
	}
	appState[authtypes.ModuleName] = authGenStateBz

	bankGenStateBz, err := clientCtx.Codec.MarshalJSON(bankGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal bank genesis state: %w", err)
	}
	appState[banktypes.ModuleName] = bankGenStateBz
	return nil
}

// Mutates the shared in-memory app state; delegations are returned so the caller
// can wire them into the staking module.
func (va Accounts) appendVestingAccounts(
	ctx context.Context,
	encodingConfig encoding.EncodingConfig,
	clientCtx client.Context,
	validatorsReference map[string]ValidatorAddresses,
	appState map[string]json.RawMessage,
) (delegations []stakingtypes.Delegation, err error) {
	claims, err := va.claimRepository.GetClaims(ctx, encodingConfig)
	if err != nil {
		return nil, err
	}
	grants, err := va.grantRepository.GetGrants(ctx, encodingConfig)
	if err != nil {
		return nil, err
	}

	authGenState, bankGenState, accs, err := loadAuthBankState(clientCtx, appState)
	if err != nil {
		return nil, err
	}

	hrp := va.cfg.AddressPrefix
	denom := va.cfg.BondDenom

	// --- claims: delayed vesting, optional immediate delegation ---
	sort.SliceStable(claims, func(i, j int) bool {
		return claims[i].Address() < claims[j].Address()
	})
	reserve := va.cfg.NonStaked()
	for _, claim := range claims {
		addr, err := encodingConfig.TxConfig.SigningContext().AddressCodec().StringToBytes(claim.Address())
		if err != nil {
			return nil, err
		}
		accs, err = AddCustomVestingGenesisAccount(
			claim, addr, 0, va.cfg.ClaimsVestingEnd,
			hrp, denom, reserve, encodingConfig, accs, bankGenState, true,
		)
		if err != nil {
			slog.Error(err.Error())
			return nil, err
		}
		if strings.TrimSpace(claim.DelegateTo()) != "" {
			if _, ok := validatorsReference[claim.DelegateTo()]; !ok {
				return nil, fmt.Errorf("validator reference for '%s' does not exist", claim.DelegateTo())
			}
			delegations = append(delegations, stakingtypes.Delegation{
				DelegatorAddress: claim.Address(),
				ValidatorAddress: validatorsReference[claim.DelegateTo()].OperatorAddress,
				Shares:           math.LegacyNewDec(claim.Amount() - reserve),
			})
		}
	}

	// --- grants: continuous vesting (start→end), never pre-delegated ---
	for _, grant := range grants {
		addr, err := encodingConfig.TxConfig.SigningContext().AddressCodec().StringToBytes(grant.Address())
		if err != nil {
			return nil, err
		}
		accs, err = AddCustomVestingGenesisAccount(
			grant, addr,
			va.cfg.GrantsVestingStart,
			va.cfg.GrantsVestingEnd,
			hrp, denom, reserve, encodingConfig, accs, bankGenState, false,
		)
		if err != nil {
			return nil, err
		}
	}

	if err := sealAuthBankState(clientCtx, accs, authGenState, bankGenState, appState); err != nil {
		return nil, err
	}
	return delegations, nil
}

type ValidatorAddresses struct {
	OperatorAddress  string
	DelegatorAddress string
}

func (va Accounts) appendValidators(
	ctx context.Context,
	encodingConfig encoding.EncodingConfig,
	clientCtx client.Context,
	appState map[string]json.RawMessage,
) (map[string]ValidatorAddresses, error) {
	validators, err := va.validatorRepository.GetValidators(ctx)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(validators, func(i, j int) bool {
		return validators[i].Name() < validators[j].Name()
	})

	if err := addValidatorsToGenesis(encodingConfig, clientCtx, validators, appState); err != nil {
		return nil, err
	}
	return buildValidatorReference(validators), nil
}

func addValidatorsToGenesis(
	encodingConfig encoding.EncodingConfig,
	clientCtx client.Context,
	validators []validator.Validator,
	appState map[string]json.RawMessage,
) error {
	authGenState, bankGenState, accs, err := loadAuthBankState(clientCtx, appState)
	if err != nil {
		return err
	}
	for i := range validators {
		addr, err := encodingConfig.TxConfig.SigningContext().AddressCodec().StringToBytes(validators[i].DelegatorAddress())
		if err != nil {
			return err
		}
		// Validators carry no liquid balance here; their stake lives in bonded_tokens_pool.
		accs, err = addBaseGenesisAccount(addr, "", true, accs, bankGenState)
		if err != nil {
			return err
		}
	}
	return sealAuthBankState(clientCtx, accs, authGenState, bankGenState, appState)
}

func buildValidatorReference(validators []validator.Validator) map[string]ValidatorAddresses {
	ref := make(map[string]ValidatorAddresses, len(validators))
	for i := range validators {
		ref[validators[i].Name()] = ValidatorAddresses{
			OperatorAddress:  validators[i].OperatorAddress(),
			DelegatorAddress: validators[i].DelegatorAddress(),
		}
	}
	return ref
}

func (va Accounts) appendModuleAccounts(
	_ context.Context,
	encodingConfig encoding.EncodingConfig,
	clientCtx client.Context,
	appState map[string]json.RawMessage,
) error {
	hrp := va.cfg.AddressPrefix
	denom := va.cfg.BondDenom

	type moduleEntry struct {
		address     string
		amount      int64
		permissions []string
	}

	validators, err := va.validatorRepository.GetValidators(context.Background())
	if err != nil {
		return err
	}
	var bondedTokens int64
	for i := range validators {
		if validators[i].Amount() > stdmath.MaxInt64-bondedTokens {
			return fmt.Errorf("bonded token overflow: cannot add %d to accumulated %d", validators[i].Amount(), bondedTokens)
		}
		bondedTokens += validators[i].Amount()
	}

	standardModules := []struct {
		name        string
		amount      int64
		permissions []string
	}{
		{"bonded_tokens_pool", bondedTokens, []string{authtypes.Burner, authtypes.Staking}},
		{"not_bonded_tokens_pool", 0, []string{authtypes.Burner, authtypes.Staking}},
		{"gov", 0, []string{authtypes.Burner}},
		{"distribution", 0, []string{}},
		{"mint", 0, []string{authtypes.Minter}},
		{"fee_collector", 0, []string{}},
	}

	modules := make(map[string]moduleEntry)
	for _, m := range standardModules {
		addr, err := moduleAddress(hrp, m.name)
		if err != nil {
			return err
		}
		modules[m.name] = moduleEntry{addr, m.amount, m.permissions}
	}

	// Extra modules from config (e.g. chain-specific modules like "meta")
	for _, em := range va.cfg.ExtraModules {
		addr, err := moduleAddress(hrp, em.Name)
		if err != nil {
			return fmt.Errorf("failed to compute address for extra module %s: %w", em.Name, err)
		}
		modules[em.Name] = moduleEntry{addr, 0, em.Permissions}
	}

	moduleKeys := make([]string, 0, len(modules))
	for k := range modules {
		moduleKeys = append(moduleKeys, k)
	}
	sort.Strings(moduleKeys)

	authGenState, bankGenState, accs, err := loadAuthBankState(clientCtx, appState)
	if err != nil {
		return err
	}
	for _, key := range moduleKeys {
		m := modules[key]
		addr, err := encodingConfig.TxConfig.SigningContext().AddressCodec().StringToBytes(m.address)
		if err != nil {
			return err
		}
		accs, err = AddCustomModuleGenesisAccount(
			addr,
			strconv.FormatInt(m.amount, 10)+denom,
			key,
			m.permissions,
			accs,
			bankGenState,
		)
		if err != nil {
			return err
		}
	}
	return sealAuthBankState(clientCtx, accs, authGenState, bankGenState, appState)
}

func (va Accounts) appendInitialAccounts(
	encodingConfig encoding.EncodingConfig,
	clientCtx client.Context,
	appState map[string]json.RawMessage,
) error {
	initialAccounts, err := va.initialAccountsRepo.GetInitialAccounts(context.Background(), encodingConfig)
	if err != nil {
		return err
	}
	if len(initialAccounts) == 0 {
		return fmt.Errorf("accounts.file_name CSV is empty; at least one account is required")
	}

	authGenState, bankGenState, accs, err := loadAuthBankState(clientCtx, appState)
	if err != nil {
		return err
	}
	denom := va.cfg.BondDenom
	for _, acc := range initialAccounts {
		if acc.Amount() == 0 {
			continue
		}
		addr, err := encodingConfig.TxConfig.SigningContext().AddressCodec().StringToBytes(acc.Address())
		if err != nil {
			return err
		}
		amountStr := strconv.FormatInt(acc.Amount(), 10) + denom
		accs, err = addBaseGenesisAccount(addr, amountStr, false, accs, bankGenState)
		if err != nil {
			return err
		}
	}
	return sealAuthBankState(clientCtx, accs, authGenState, bankGenState, appState)
}
