package genesis

import (
	"encoding/json"

	"cosmossdk.io/math"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
)

func (asm stateManager) fixMintParameters(appGenState map[string]json.RawMessage) error {
	var mintGenState minttypes.GenesisState
	return updateModuleState(asm.encodingConfig.Codec, appGenState, "mint", &mintGenState, func() error {
		mintGenState.Params.MintDenom = asm.cfg.BondDenom
		if v := asm.cfg.BlocksPerYear; v > 0 {
			mintGenState.Params.BlocksPerYear = uint64(v)
		}
		if v := asm.cfg.InflationRateChange; v != "" {
			mintGenState.Params.InflationRateChange = math.LegacyMustNewDecFromStr(v)
		}
		if v := asm.cfg.InflationMax; v != "" {
			mintGenState.Params.InflationMax = math.LegacyMustNewDecFromStr(v)
		}
		if v := asm.cfg.InflationMin; v != "" {
			mintGenState.Params.InflationMin = math.LegacyMustNewDecFromStr(v)
		}
		if v := asm.cfg.GoalBonded; v != "" {
			mintGenState.Params.GoalBonded = math.LegacyMustNewDecFromStr(v)
		}
		return nil
	})
}
