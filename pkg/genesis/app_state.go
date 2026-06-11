package genesis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/encoding"
)

type stateManager struct {
	claimRepository        ClaimRepository
	grantRepository        GrantRepository
	initialAccountsRepo    InitialAccountsRepository
	validatorRepository    ValidatorRepository
	authzGrantRepository   AuthzGrantRepository
	feeAllowanceRepository FeeAllowanceRepository
	accounts               *accountsBuilder
	appGenState            map[string]json.RawMessage
	appGenesis             *genutiltypes.AppGenesis
	encodingConfig         encoding.EncodingConfig
	clientCtx              client.Context
	cfg                    ChainConfig
}

func newAppStateManager(
	cfg ChainConfig,
	claimRepository ClaimRepository,
	grantRepository GrantRepository,
	initialAccountsRepo InitialAccountsRepository,
	validatorRepository ValidatorRepository,
	authzGrantRepository AuthzGrantRepository,
	feeAllowanceRepository FeeAllowanceRepository,
	appGenState map[string]json.RawMessage,
	appGenesis *genutiltypes.AppGenesis,
	encodingConfig encoding.EncodingConfig,
	clientCtx client.Context,
) *stateManager {
	return &stateManager{
		claimRepository:        claimRepository,
		grantRepository:        grantRepository,
		initialAccountsRepo:    initialAccountsRepo,
		validatorRepository:    validatorRepository,
		authzGrantRepository:   authzGrantRepository,
		feeAllowanceRepository: feeAllowanceRepository,
		accounts:               newAccountsBuilder(cfg, claimRepository, grantRepository, initialAccountsRepo, validatorRepository),
		appGenState:            appGenState,
		appGenesis:             appGenesis,
		encodingConfig:         encodingConfig,
		clientCtx:              clientCtx,
		cfg:                    cfg,
	}
}

func (asm stateManager) setupAppState(ctx context.Context) (*genutiltypes.AppGenesis, map[string]int64, error) {
	slog.Info("Fixing governance parameters...")
	if err := asm.fixGovernanceParameters(asm.appGenState); err != nil {
		return nil, nil, err
	}

	slog.Info("Fixing mint parameters...")
	if err := asm.fixMintParameters(asm.appGenState); err != nil {
		return nil, nil, err
	}

	slog.Info("Appending module accounts...")
	if err := asm.accounts.appendModuleAccounts(ctx, asm.encodingConfig, asm.clientCtx, asm.appGenState); err != nil {
		return nil, nil, err
	}

	slog.Info("Fetching validator shares...")
	shares, err := asm.accounts.fetchValidatorsShares(asm.encodingConfig)
	if err != nil {
		return nil, nil, err
	}

	slog.Info("Appending validators...")
	validatorsReference, err := asm.accounts.appendValidators(ctx, asm.encodingConfig, asm.clientCtx, asm.appGenState)
	if err != nil {
		return nil, nil, err
	}

	slog.Info("Appending initial accounts...")
	if err := asm.accounts.appendInitialAccounts(asm.encodingConfig, asm.clientCtx, asm.appGenState); err != nil {
		return nil, nil, err
	}

	slog.Info("Appending claims and grants...")
	delegations, err := asm.accounts.appendVestingAccounts(
		ctx, asm.encodingConfig, asm.clientCtx, validatorsReference, asm.appGenState)
	if err != nil {
		return nil, nil, err
	}

	slog.Info("Configuring module states...")
	if err := asm.configureModuleStates(ctx, delegations, shares); err != nil {
		return nil, nil, err
	}

	slog.Info("Validating total supply...")
	if err := asm.validateSupply(); err != nil {
		return nil, nil, fmt.Errorf("supply validation failed: %w", err)
	}

	slog.Info("Sealing final genesis state...")
	genesisTime := time.Unix(asm.cfg.GenesisTime, 0).UTC()
	if err := sealAppGenesis(asm.appGenState, asm.appGenesis, genesisTime); err != nil {
		return nil, nil, err
	}

	slog.Info("setupAppState completed successfully.")
	return asm.appGenesis, shares, nil
}

func (asm stateManager) configureModuleStates(ctx context.Context, delegations []stakingtypes.Delegation, shares map[string]int64) error {
	slog.Info("Configuring staking parameters...")
	if err := asm.setStakingState(asm.appGenState, delegations, shares); err != nil {
		return err
	}

	slog.Info("Configuring denomination metadata...")
	if err := asm.setDenominationMetadata(); err != nil {
		return err
	}

	slog.Info("Configuring distribution parameters...")
	if err := asm.setDistribution(asm.appGenState, delegations); err != nil {
		return err
	}

	slog.Info("Configuring slashing parameters...")
	if err := asm.setSlashingState(asm.appGenState); err != nil {
		return err
	}

	slog.Info("Configuring authz grants...")
	if err := asm.setAuthzState(ctx, asm.appGenState); err != nil {
		return err
	}

	slog.Info("Configuring fee allowances...")
	if err := asm.setFeegrantState(ctx, asm.appGenState); err != nil {
		return err
	}

	return nil
}
