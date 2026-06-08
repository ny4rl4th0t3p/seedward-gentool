package app

import (
	"encoding/json"
	"fmt"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

func (asm StateManager) fixGovernanceParameters(appGenState map[string]json.RawMessage) error {
	var govGenState govv1.GenesisState
	return updateModuleState(asm.encodingConfig.Codec, appGenState, "gov", &govGenState, func() error {
		if govGenState.Params == nil {
			defaults := govv1.DefaultParams()
			govGenState.Params = &defaults
		}
		denom := asm.cfg.BondDenom
		if v := asm.cfg.GovMinDepositAmount; v > 0 {
			govGenState.Params.MinDeposit = sdk.Coins{{Denom: denom, Amount: math.NewInt(v)}}
		}
		if v := asm.cfg.GovVotingPeriod; v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("invalid gov.voting_period %q: %w", v, err)
			}
			govGenState.Params.VotingPeriod = &d
		}
		if v := asm.cfg.GovExpeditedMinDepositAmount; v > 0 {
			govGenState.Params.ExpeditedMinDeposit = sdk.Coins{{Denom: denom, Amount: math.NewInt(v)}}
		}
		if v := asm.cfg.GovExpeditedVotingPeriod; v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("invalid gov.expedited_voting_period %q: %w", v, err)
			}
			govGenState.Params.ExpeditedVotingPeriod = &d
		}
		return nil
	})
}
