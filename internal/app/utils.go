package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdkcodec "github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/internal/domain/vesting_account"
	"github.com/ny4rl4th0t3p/cosmos-genesis-tool/internal/encoding"
)

const (
	InvalidVestingErr = "invalid vesting parameters; must supply start and end time or end time"
)

// Set sdk.GetConfig() bech32 prefixes and sdk.DefaultBondDenom before calling this.
func LoadGenesis(
	path string, cfg ChainConfig,
) (encoding.EncodingConfig, client.Context, map[string]json.RawMessage, *genutiltypes.AppGenesis, error) {
	encodingConfig := encoding.NewEncodingConfig()

	appState, appGenesis, err := genutiltypes.GenesisStateFromGenFile(path)
	if err != nil {
		return encoding.EncodingConfig{}, client.Context{}, nil, nil, fmt.Errorf("failed to read genesis file %s: %w", path, err)
	}

	// Override genesis metadata from config; the baseline file values are ignored.
	appGenesis.GenesisTime = time.Unix(cfg.GenesisTime, 0).UTC()
	appGenesis.AppName = cfg.AppName
	appGenesis.AppVersion = cfg.AppVersion
	appGenesis.ChainID = cfg.ChainID
	appGenesis.InitialHeight = cfg.InitialHeight

	clientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithLegacyAmino(encodingConfig.Amino).
		WithTxConfig(encodingConfig.TxConfig).
		WithInput(os.Stdin).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithHomeDir(".").
		WithViper("").
		WithChainID(cfg.ChainID)

	return encodingConfig, clientCtx, appState, appGenesis, nil
}

func moduleAddress(hrp, moduleName string) (string, error) {
	addr, err := bech32.ConvertAndEncode(hrp, authtypes.NewModuleAddress(moduleName))
	if err != nil {
		return "", fmt.Errorf("failed to compute module address for %s: %w", moduleName, err)
	}
	return addr, nil
}

func saveGenesis(
	appGenState map[string]json.RawMessage,
	appGenesis *genutiltypes.AppGenesis,
	genesisTime time.Time,
	outputPath string,
) error {
	appStateJSON, err := json.Marshal(appGenState)
	if err != nil {
		return errorsmod.Wrap(err, "failed to marshal app state")
	}
	appGenesis.AppState = appStateJSON
	appGenesis.GenesisTime = genesisTime
	return appGenesis.SaveAs(outputPath)
}

// addBaseGenesisAccount adds a plain base account (and its balance) to the
// in-memory auth/bank state, mirroring genutil.AddGenesisAccount without
// touching disk. An empty amountStr (e.g. validators, whose stake lives in the
// bonded_tokens_pool) adds the account with no balance or supply change.
func addBaseGenesisAccount(
	accAddr sdk.AccAddress,
	amountStr string,
	appendAccount bool,
	accs authtypes.GenesisAccounts,
	bankGenState *banktypes.GenesisState,
) (authtypes.GenesisAccounts, error) {
	coins, err := sdk.ParseCoinsNormalized(amountStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse coins: %w", err)
	}

	if accs.Contains(accAddr) {
		if !appendAccount {
			return nil, fmt.Errorf("account %s already exists", accAddr)
		}
	} else {
		accs = append(accs, authtypes.NewBaseAccount(accAddr, nil, 0, 0))
	}

	if coins.IsZero() {
		return accs, nil
	}

	balance := banktypes.Balance{Address: accAddr.String(), Coins: coins.Sort()}
	if err := updateBalances(accAddr, balance, coins, bankGenState, appendAccount); err != nil {
		return nil, err
	}
	bankGenState.Supply = bankGenState.Supply.Add(coins...)
	return accs, nil
}

func updateModuleState(
	cdc sdkcodec.Codec,
	appGenState map[string]json.RawMessage,
	moduleName string,
	state sdkcodec.ProtoMarshaler, //nolint:staticcheck // Cosmos SDK v0.50 still exposes this; proto.Message migration is a separate task
	updater func() error,
) error {
	raw, ok := appGenState[moduleName]
	if !ok {
		return fmt.Errorf("%s module not found in genesis state", moduleName)
	}
	if err := cdc.UnmarshalJSON(raw, state); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", moduleName, err)
	}
	if err := updater(); err != nil {
		return err
	}
	bz, err := cdc.MarshalJSON(state)
	if err != nil {
		return fmt.Errorf("failed to marshal %s genesis state: %w", moduleName, err)
	}
	appGenState[moduleName] = bz
	return nil
}

func AddCustomVestingGenesisAccount(
	vestingAccount vesting_account.VestingAccount,
	accAddr sdk.AccAddress,
	vestingStart, vestingEnd int64,
	hrp, denom string,
	nonStakedReserve int64,
	encodingConfig encoding.EncodingConfig,
	accs authtypes.GenesisAccounts,
	bankGenState *banktypes.GenesisState,
	appendAcct bool,
) (authtypes.GenesisAccounts, error) {
	genAccount, balances, err := createVestingAccount(vestingAccount, accAddr, vestingStart, vestingEnd, denom, nonStakedReserve)
	if err != nil {
		return nil, err
	}

	addr, err := encodingConfig.TxConfig.SigningContext().AddressCodec().StringToBytes(vestingAccount.Address())
	if err != nil {
		return nil, err
	}

	if err := allocateDelegatedFunds(
		vestingAccount, addr, accAddr, balances, hrp, encodingConfig, bankGenState, appendAcct, denom, nonStakedReserve,
	); err != nil {
		return nil, err
	}

	accs = append(accs, genAccount)
	bankGenState.Supply = bankGenState.Supply.Add(balances.Coins...)
	return accs, nil
}

func createVestingAccount(
	vestingAccount vesting_account.VestingAccount,
	accAddr sdk.AccAddress,
	vestingStart, vestingEnd int64,
	denom string,
	nonStakedReserve int64,
) (authtypes.GenesisAccount, banktypes.Balance, error) {
	coins, err := sdk.ParseCoinsNormalized(strconv.FormatInt(vestingAccount.Amount(), 10) + denom)
	if err != nil {
		return nil, banktypes.Balance{}, fmt.Errorf("failed to parse coins: %w", err)
	}

	balances := banktypes.Balance{Address: accAddr.String(), Coins: coins.Sort()}
	baseAccount := authtypes.NewBaseAccount(accAddr, nil, 0, 0)
	baseVestingAccount, err := authvesting.NewBaseVestingAccount(baseAccount, coins.Sort(), vestingEnd)
	if err != nil {
		return nil, banktypes.Balance{}, fmt.Errorf("failed to create base vesting account: %w", err)
	}
	if baseVestingAccount.OriginalVesting.IsAnyGT(balances.Coins) {
		return nil, banktypes.Balance{}, errors.New("vesting amount cannot be greater than total amount")
	}

	if vestingAccount.DelegateTo() != "" {
		// A delegating account must keep a liquid reserve; without it the Sub below
		// would go negative (panic), and the account would have no balance to pay gas.
		if vestingAccount.Amount() <= nonStakedReserve {
			return nil, banktypes.Balance{}, fmt.Errorf(
				"vesting account %s delegating to %s: amount %d must exceed the non_staked_amount reserve %d",
				vestingAccount.Address(), vestingAccount.DelegateTo(), vestingAccount.Amount(), nonStakedReserve,
			)
		}
		baseVestingAccount.DelegatedVesting = baseVestingAccount.GetOriginalVesting().Sub(sdk.Coin{
			Denom:  denom,
			Amount: math.NewInt(nonStakedReserve),
		})
	}

	var genAccount authtypes.GenesisAccount
	switch {
	case vestingStart != 0 && vestingEnd != 0:
		genAccount = authvesting.NewContinuousVestingAccountRaw(baseVestingAccount, vestingStart)
	case vestingEnd != 0:
		genAccount = authvesting.NewDelayedVestingAccountRaw(baseVestingAccount)
	default:
		return nil, banktypes.Balance{}, errors.New(InvalidVestingErr)
	}
	if err := genAccount.Validate(); err != nil {
		return nil, banktypes.Balance{}, fmt.Errorf("failed to validate new genesis account: %w", err)
	}

	return genAccount, balances, nil
}

func allocateDelegatedFunds(
	vestingAccount vesting_account.VestingAccount,
	addr sdk.AccAddress,
	accAddr sdk.AccAddress,
	balances banktypes.Balance,
	hrp string,
	encodingConfig encoding.EncodingConfig,
	bankGenState *banktypes.GenesisState,
	appendAcct bool,
	denom string,
	nonStakedReserve int64,
) error {
	if vestingAccount.DelegateTo() == "" {
		return updateBalances(addr, balances, balances.Coins, bankGenState, appendAcct)
	}

	bondedAddr, err := moduleAddress(hrp, "bonded_tokens_pool")
	if err != nil {
		return err
	}
	bondedModuleAddr, err := encodingConfig.TxConfig.SigningContext().AddressCodec().StringToBytes(bondedAddr)
	if err != nil {
		return err
	}
	unstakedCoin := sdk.Coin{Denom: denom, Amount: math.NewInt(nonStakedReserve)}
	stakedCoins := balances.Coins.Sub(unstakedCoin)
	if err := updateBalances(bondedModuleAddr, balances, stakedCoins, bankGenState, appendAcct); err != nil {
		return err
	}
	unstakedCoins := sdk.Coins{unstakedCoin}
	return updateBalances(addr, banktypes.Balance{Address: accAddr.String(), Coins: unstakedCoins}, unstakedCoins, bankGenState, true)
}

func updateBalances(
	accAddr sdk.AccAddress, balances banktypes.Balance, coins sdk.Coins, bankGenState *banktypes.GenesisState, appendAcct bool,
) error {
	for idx, acc := range bankGenState.Balances {
		if acc.Address == accAddr.String() {
			if !appendAcct {
				return fmt.Errorf("account %s already exists. Use `append` flag to append account at existing address", accAddr)
			}
			updatedCoins := acc.Coins.Add(coins...)
			bankGenState.Balances[idx] = banktypes.Balance{Address: accAddr.String(), Coins: updatedCoins.Sort()}
			return nil
		}
	}
	bankGenState.Balances = append(bankGenState.Balances, balances)
	return nil
}

// AddCustomModuleGenesisAccount adds a module account (and its balance) to the
// in-memory auth/bank state. The caller is responsible for loading accs/
// bankGenState and sealing them back into the genesis app state.
func AddCustomModuleGenesisAccount(
	accAddr sdk.AccAddress,
	amountStr,
	moduleName string,
	permissions []string,
	accs authtypes.GenesisAccounts,
	bankGenState *banktypes.GenesisState,
) (authtypes.GenesisAccounts, error) {
	coins, err := sdk.ParseCoinsNormalized(amountStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse coins: %w", err)
	}
	genAccount := authtypes.NewEmptyModuleAccount(moduleName, permissions...)
	if err := genAccount.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate new genesis account: %w", err)
	}

	if accs.Contains(accAddr) {
		return nil, fmt.Errorf("account %s already exists", accAddr)
	}
	accs = append(accs, genAccount)

	balance := banktypes.Balance{Address: accAddr.String(), Coins: coins.Sort()}
	bankGenState.Balances = append(bankGenState.Balances, balance)
	bankGenState.Supply = bankGenState.Supply.Add(balance.Coins...)
	return accs, nil
}
