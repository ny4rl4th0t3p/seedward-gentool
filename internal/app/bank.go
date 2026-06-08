package app

import (
	"fmt"

	"cosmossdk.io/math"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (asm StateManager) setDenominationMetadata() error {
	base := asm.cfg.DenomBase
	if base == "" {
		// No denom metadata configured; preserve baseline.
		return nil
	}

	bankGenState := banktypes.GetGenesisStateFromAppState(asm.clientCtx.Codec, asm.appGenState)

	display := asm.cfg.DenomDisplay
	symbol := asm.cfg.DenomSymbol
	description := asm.cfg.DenomDescription
	exponent := asm.cfg.DenomExponent
	aliases := asm.cfg.DenomAliases

	denomUnits := []*banktypes.DenomUnit{
		{Denom: base, Exponent: 0, Aliases: aliases},
	}
	if display != "" && display != base {
		denomUnits = append(denomUnits, &banktypes.DenomUnit{Denom: display, Exponent: exponent})
	}

	metadata := banktypes.Metadata{
		Description: description,
		DenomUnits:  denomUnits,
		Base:        base,
		Display:     display,
		Name:        symbol,
		Symbol:      symbol,
	}
	bankGenState.DenomMetadata = []banktypes.Metadata{metadata}

	bankStateBz, err := asm.clientCtx.Codec.MarshalJSON(bankGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal bank genesis state: %w", err)
	}
	asm.appGenState["bank"] = bankStateBz
	return nil
}

func (asm StateManager) validateSupply() error {
	bankGenState := banktypes.GetGenesisStateFromAppState(asm.clientCtx.Codec, asm.appGenState)
	supply := bankGenState.Supply.AmountOf(asm.cfg.BondDenom)
	totalSupply := math.NewInt(asm.cfg.TotalSupply)
	if !supply.Equal(totalSupply) {
		return fmt.Errorf("total supply mismatch: got %s, expected %s", supply, totalSupply)
	}
	return nil
}
