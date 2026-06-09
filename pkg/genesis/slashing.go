package genesis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
)

const valconsHRPSuffix = "valcons"

func (asm stateManager) setSlashingState(appGenState map[string]json.RawMessage) error {
	validators, err := asm.validatorRepository.GetValidators(context.Background())
	if err != nil {
		return err
	}

	hrp := asm.cfg.AddressPrefix
	valconsHRP := hrp + valconsHRPSuffix

	var slashingGenState slashingtypes.GenesisState
	return updateModuleState(asm.encodingConfig.Codec, appGenState, "slashing", &slashingGenState, func() error {
		if v := asm.cfg.SignedBlocksWindow; v > 0 {
			slashingGenState.Params.SignedBlocksWindow = v
		}
		if v := asm.cfg.MinSignedPerWindow; v != "" {
			slashingGenState.Params.MinSignedPerWindow = math.LegacyMustNewDecFromStr(v)
		}
		if v := asm.cfg.DowntimeJailDurationSeconds; v > 0 {
			slashingGenState.Params.DowntimeJailDuration = time.Duration(v) * time.Second
		}
		if v := asm.cfg.SlashFractionDoubleSign; v != "" {
			slashingGenState.Params.SlashFractionDoubleSign = math.LegacyMustNewDecFromStr(v)
		}
		if v := asm.cfg.SlashFractionDowntime; v != "" {
			slashingGenState.Params.SlashFractionDowntime = math.LegacyMustNewDecFromStr(v)
		}

		var signingInfos []slashingtypes.SigningInfo
		var missedBlocks []slashingtypes.ValidatorMissedBlocks
		for i := range validators {
			bech32Addr, err := bech32.ConvertAndEncode(valconsHRP, validators[i].ConsensusAddress())
			if err != nil {
				return fmt.Errorf("failed to encode valcons address: %w", err)
			}
			signingInfos = append(signingInfos, slashingtypes.SigningInfo{
				Address: bech32Addr,
				ValidatorSigningInfo: slashingtypes.ValidatorSigningInfo{
					Address:             bech32Addr,
					StartHeight:         0,
					IndexOffset:         0,
					JailedUntil:         time.Time{},
					Tombstoned:          false,
					MissedBlocksCounter: 0,
				},
			})
			missedBlocks = append(missedBlocks, slashingtypes.ValidatorMissedBlocks{
				Address:      bech32Addr,
				MissedBlocks: []slashingtypes.MissedBlock{},
			})
		}
		slashingGenState.SigningInfos = signingInfos
		slashingGenState.MissedBlocks = missedBlocks
		return nil
	})
}
