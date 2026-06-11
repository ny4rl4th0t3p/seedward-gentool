package genesis

import (
	"context"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/accounts"
	genesisauthz "github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/authz"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/encoding"
	genesisfeegrant "github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/feegrant"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/validator"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/vestingaccount"
)

type ClaimRepository interface {
	GetClaims(ctx context.Context, encodingConfig encoding.EncodingConfig) ([]vestingaccount.Claim, error)
}

type InitialAccountsRepository interface {
	GetInitialAccounts(ctx context.Context, encodingConfig encoding.EncodingConfig) ([]accounts.InitialAccount, error)
}

type GrantRepository interface {
	GetGrants(ctx context.Context, encodingConfig encoding.EncodingConfig) ([]vestingaccount.Grant, error)
}

type ValidatorRepository interface {
	GetValidators(ctx context.Context) ([]validator.Validator, error)
}

type AuthzGrantRepository interface {
	GetAuthzGrants(ctx context.Context, encodingConfig encoding.EncodingConfig) ([]genesisauthz.AuthzGrant, error)
}

type FeeAllowanceRepository interface {
	GetFeeAllowances(ctx context.Context, encodingConfig encoding.EncodingConfig) ([]genesisfeegrant.FeeAllowance, error)
}
