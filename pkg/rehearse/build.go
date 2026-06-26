package rehearse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/csv"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/encoding"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/gentx"
)

// prepared is a materialized rehearsal workdir: the present allocation files written to
// disk plus a baseline genesis from `<chaind> init`. Both the build check (real gentxs) and
// the boot genesis (substitute gentxs) build from it, differing only in the gentx directory.
type prepared struct {
	dir          string // temp workdir root (caller removes)
	baseGenesis  []byte
	repos        genesis.Repositories // CSV repos wired; Validators filled per gentx set
	realGentxDir string               // the real gentxs, materialized (build check + self-deleg sum)
}

// allocWiring describes an optional allocation type: written and wired only when present.
// gentool treats a nil repository as "no records", so absent types need no empty file.
type allocWiring struct {
	typ  AllocationType
	file string
	wire func(r *genesis.Repositories, path string, mods map[string]bool)
}

var optionalAllocations = []allocWiring{
	{AllocationClaims, "claims.csv", func(r *genesis.Repositories, p string, m map[string]bool) {
		r.Claims = csv.NewCSVClaimRepository(p, m)
	}},
	{AllocationGrants, "grants.csv", func(r *genesis.Repositories, p string, m map[string]bool) {
		r.Grants = csv.NewCSVGrantRepository(p, m)
	}},
	{AllocationAuthz, "authz.csv", func(r *genesis.Repositories, p string, m map[string]bool) {
		r.AuthzGrants = csv.NewCSVAuthzGrantRepository(p, m)
	}},
	{AllocationFeegrant, "feegrant.csv", func(r *genesis.Repositories, p string, m map[string]bool) {
		r.FeeAllowances = csv.NewCSVFeeAllowanceRepository(p, m)
	}},
}

// materialize writes the input's allocation files and a `<chaind> init` baseline into a fresh
// temp workdir and wires the CSV repositories (mirroring `gentool create`). accounts file is
// required and must be present (the caller verifies); optional types are written and wired
// only when provided — absent ones leave a nil repository (a clean skip in gentool).
//
// Ownership: on success the returned prep.dir belongs to the caller, which must remove it
// (e.g. defer os.RemoveAll(prep.dir)); on any error materialize cleans the workdir up itself.
// Errors here are infrastructure faults (temp dir, binary init) → ERROR.
func materialize(ctx context.Context, in Input) (*prepared, error) {
	dir, err := os.MkdirTemp("", "rehearse-")
	if err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}
	// Remove the workdir unless we hand it off successfully; on success the caller
	// owns prep.dir and removes it after the run. Guards every error path, including
	// ones added later.
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(dir)
		}
	}()

	mods := computeModuleAddresses(in.Config.AddressPrefix, in.Config.ExtraModules)

	accountsPath := filepath.Join(dir, "accounts.csv")
	if err := os.WriteFile(accountsPath, in.Allocations[AllocationAccounts], 0o600); err != nil {
		return nil, fmt.Errorf("write accounts.csv: %w", err)
	}
	repos := genesis.Repositories{
		InitialAccounts: csv.NewCSVInitialAccountsRepository(accountsPath, mods),
	}
	for _, a := range optionalAllocations {
		b, present := in.Allocations[a.typ]
		if !present {
			continue
		}
		fpath := filepath.Join(dir, a.file)
		if err := os.WriteFile(fpath, b, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", a.file, err)
		}
		a.wire(&repos, fpath, mods)
	}

	home := filepath.Join(dir, "baseline")
	if _, err := (chainBinary{in.BinaryPath}).run(ctx, "init", "rehearse-node", "--chain-id", in.Config.ChainID, "--home", home); err != nil {
		return nil, fmt.Errorf("chaind init: %w", err)
	}
	base, err := os.ReadFile(filepath.Join(home, "config", "genesis.json"))
	if err != nil {
		return nil, fmt.Errorf("read baseline genesis: %w", err)
	}

	realGentxDir := filepath.Join(dir, "gentx-real")
	if err := writeGentxs(realGentxDir, in.Gentxs); err != nil {
		return nil, err
	}

	committed = true
	return &prepared{dir: dir, baseGenesis: base, repos: repos, realGentxDir: realGentxDir}, nil
}

// buildGenesis assembles the genesis from the materialized allocations + the gentxs in
// gentxDir. Used twice: real gentxs (assembly check) and substitute gentxs (boot).
func (p *prepared) buildGenesis(ctx context.Context, cfg genesis.ChainConfig, gentxDir string) (*genutiltypes.AppGenesis, error) {
	repos := p.repos
	repos.Validators = gentx.NewValidatorRepository(gentxDir, cfg.AddressPrefix)
	return genesis.Build(ctx, p.baseGenesis, cfg, repos)
}

// buildCheck assembles the genesis from the REAL gentxs (materialized by materialize).
// Failure means the approved input set does not assemble (e.g. supply mismatch) → FAIL:
// build. The result is discarded; the bootable genesis is built separately on substitutes.
func buildCheck(ctx context.Context, in Input, p *prepared) error {
	_, err := p.buildGenesis(ctx, in.Config, p.realGentxDir)
	return err
}

// writeGentxs writes each gentx JSON as gentx-<i>.json into dir (created if needed).
func writeGentxs(dir string, gentxs [][]byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create gentx dir: %w", err)
	}
	for i, g := range gentxs {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("gentx-%d.json", i)), g, 0o600); err != nil {
			return fmt.Errorf("write gentx %d: %w", i, err)
		}
	}
	return nil
}

// computeModuleAddresses returns the bech32 module account addresses (standard and extra),
// which the CSV repos skip when they appear as a record's first field (mirrors gentool).
func computeModuleAddresses(hrp string, extra []genesis.ExtraModule) map[string]bool {
	names := append([]string{}, encoding.StandardModuleNames...)
	for _, em := range extra {
		names = append(names, em.Name)
	}
	return encoding.ModuleAddresses(hrp, names)
}
