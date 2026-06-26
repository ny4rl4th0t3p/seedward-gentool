//go:build integration_boot

// Boot integration test: exercises the full engine (verify → build → substitute → boot →
// assert) against a REAL chain binary. It is excluded from the default build/test by the
// integration_boot tag, so normal `go test ./...` needs no binary.
//
// Run it with a pre-provisioned chaind on disk:
//
//	SEEDWARD_REHEARSE_CHAIND=/path/to/gaiad \
//	  go test -tags integration_boot -run TestEngine_BootAndAssert_Integration -v ./pkg/rehearse/
//
// The fixture builds 5 real validators and collapses them onto 2 substitutes, so the
// many-to-few collapse (self-delegation partitioning + real→substitute fan-in) and the claim
// delegation remap are genuinely exercised — not a 1:1 pass-through. It mirrors
// tests/smoke/smoke.sh otherwise (1 account, claims, grant, authz, feegrant, denom metadata,
// full params), but feeds the engine a FRESH base genesis, so the validators carry zero liquid
// balance and the total supply excludes their funding.

package rehearse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
)

// integrationRealValidators is the real validator count the fixture generates; it must exceed
// the substitute count below so the collapse is actually tested.
const integrationRealValidators = 5
const integrationSubstitutes = 2

func TestEngine_BootAndAssert_Integration(t *testing.T) {
	binPath := os.Getenv("SEEDWARD_REHEARSE_CHAIND")
	if binPath == "" {
		t.Skip("SEEDWARD_REHEARSE_CHAIND not set; skipping boot integration test")
	}

	in := buildIntegrationInput(t, binPath)
	e := New(NewProcessRuntime(binPath), WithValidators(integrationSubstitutes), WithBootWait(2*time.Minute))

	res, err := e.Run(context.Background(), in)
	require.NoError(t, err)
	t.Logf("rehearsal report:\n%s", res.Report())

	for _, s := range res.Steps {
		assert.NotEqualf(t, StepFail, s.Status, "%s: %s", s.Name, s.Detail)
	}
	require.Equalf(t, OutcomePass, res.Outcome, "summary: %s", res.Summary)

	// Collapse + mapping: the 5 real validators must fold onto 2 substitutes, with every real
	// moniker remapped onto one of them (so several reals share a substitute).
	require.NotNil(t, res.Substitution)
	assert.Len(t, res.Substitution.Validators, integrationSubstitutes)
	assert.Len(t, res.Substitution.RealToSubstitute, integrationRealValidators)
	subMonikers := make(map[string]bool, len(res.Substitution.Validators))
	for _, v := range res.Substitution.Validators {
		subMonikers[v.Moniker] = true
	}
	for realMoniker, sub := range res.Substitution.RealToSubstitute {
		assert.Truef(t, subMonikers[sub], "real %s mapped to unknown substitute %s", realMoniker, sub)
	}
}

// buildIntegrationInput runs the chain binary to generate real keys, gentxs and allocation
// CSVs, then assembles them into an engine Input. The numbers mirror smoke.sh, minus the
// validator liquid funding (the engine builds on a fresh base genesis).
func buildIntegrationInput(t *testing.T, binPath string) Input {
	t.Helper()
	ctx := context.Background()
	bin := chainBinary{binPath}
	work := t.TempDir()
	home := filepath.Join(work, "keyhome")

	const (
		chainID        = "rehearse-it-1"
		denom          = "uatom"
		genesisTime    = int64(1735987170)
		valSelfDeleg   = int64(1000000)
		accountBalance = int64(1000000)
		claim1Amount   = int64(1000000) // delegates to validator 0
		claim2Amount   = int64(500000)  // no delegation
		claim3Amount   = int64(1000000) // delegates to validator 1 (→ a different substitute)
		grant1Amount   = int64(2000000)
		communityPool  = int64(500000)
		feegrantLimit  = int64(5000000)
		vestingEnd     = int64(1900000000)
	)
	monikers := make([]string, integrationRealValidators)
	for i := range monikers {
		monikers[i] = fmt.Sprintf("validator-%d", i)
	}

	run := func(args ...string) string {
		out, err := bin.run(ctx, args...)
		require.NoErrorf(t, err, "cmd %v\n%s", args, out)
		return strings.TrimSpace(string(out))
	}
	keyring := []string{"--keyring-backend", "test", "--home", home}
	addKey := func(name string) string {
		run(append([]string{"keys", "add", name}, keyring...)...)
		return run(append([]string{"keys", "show", name, "-a"}, keyring...)...)
	}

	run("init", "fixture", "--chain-id", chainID, "--home", home)

	// Real validator gentxs — each needs a distinct consensus key, so validators after the
	// first borrow a freshly-init'd home's pubkey via --pubkey (mirrors smoke.sh's node2).
	var gentxs [][]byte
	for i, moniker := range monikers {
		valKey := fmt.Sprintf("validator%d", i)
		run(append([]string{"keys", "add", valKey}, keyring...)...)
		valAddr := run(append([]string{"keys", "show", valKey, "-a"}, keyring...)...)
		run("genesis", "add-genesis-account", valAddr, fmt.Sprintf("%d%s", valSelfDeleg, denom), "--home", home)

		gentxFile := filepath.Join(work, valKey+".json")
		args := []string{
			"genesis", "gentx", valKey, fmt.Sprintf("%d%s", valSelfDeleg, denom),
			"--chain-id", chainID, "--moniker", moniker,
			"--commission-rate", "0.10", "--commission-max-rate", "0.20", "--commission-max-change-rate", "0.01",
			"--output-document", gentxFile,
		}
		if i > 0 {
			consHome := filepath.Join(work, "cons-"+valKey)
			run("init", "cons-"+valKey, "--chain-id", chainID, "--home", consHome)
			args = append(args, "--pubkey", run("comet", "show-validator", "--home", consHome))
		}
		run(append(args, keyring...)...)

		bz, err := os.ReadFile(gentxFile)
		require.NoError(t, err)
		gentxs = append(gentxs, bz)
	}

	acc1 := addKey("account1")
	claim1 := addKey("claim1")
	claim2 := addKey("claim2")
	claim3 := addKey("claim3")
	grant1 := addKey("grant1")

	totalSupply := int64(len(monikers))*valSelfDeleg + accountBalance +
		claim1Amount + claim2Amount + claim3Amount + grant1Amount + communityPool

	alloc := map[AllocationType][]byte{
		AllocationAccounts: []byte(fmt.Sprintf("%s,%d\n", acc1, accountBalance)),
		AllocationClaims: []byte(fmt.Sprintf("%s,%d,%s\n%s,%d\n%s,%d,%s\n",
			claim1, claim1Amount, monikers[0],
			claim2, claim2Amount,
			claim3, claim3Amount, monikers[1])),
		AllocationGrants:   []byte(fmt.Sprintf("%s,%d\n", grant1, grant1Amount)),
		AllocationAuthz:    []byte(fmt.Sprintf("%s,%s,/cosmos.bank.v1beta1.MsgSend\n", acc1, claim1)),
		AllocationFeegrant: []byte(fmt.Sprintf("%s,%s,%d\n", acc1, claim2, feegrantLimit)),
	}

	cfg := genesis.ChainConfig{
		ChainID: chainID, AppName: "gaia", AppVersion: "0.0.1", GenesisTime: genesisTime, InitialHeight: 1,
		AddressPrefix: "cosmos", BondDenom: denom,
		TotalSupply:      totalSupply,
		ClaimsVestingEnd: vestingEnd, GrantsVestingStart: genesisTime, GrantsVestingEnd: vestingEnd,
		DenomBase: denom, DenomDisplay: "ATOM", DenomSymbol: "ATOM",
		DenomDescription: "The native staking token of the Cosmos Hub",
		DenomExponent:    6, DenomAliases: []string{"microatom"},
		UnbondingTimeSeconds: 1814400, MaxValidators: 100, MaxEntries: 7,
		HistoricalEntries: 10000, MinCommissionRate: "0.05",
		BlocksPerYear:       6311520,
		GovMinDepositAmount: 10000000, GovVotingPeriod: "172800s",
		GovExpeditedMinDepositAmount: 50000000, GovExpeditedVotingPeriod: "86400s",
		SignedBlocksWindow: 10000, MinSignedPerWindow: "0.5", DowntimeJailDurationSeconds: 600,
		SlashFractionDoubleSign: "0.05", SlashFractionDowntime: "0.0001",
		CommunityPoolAmount: communityPool,
	}

	binBytes, err := os.ReadFile(binPath)
	require.NoError(t, err)

	return Input{
		Config:       cfg,
		Allocations:  alloc,
		Gentxs:       gentxs,
		BinaryPath:   binPath,
		BinarySHA256: sha256Hex(binBytes),
	}
}
