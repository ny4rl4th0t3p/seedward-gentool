package rehearse

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
	gcsv "github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/csv"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/gentx"
)

// substitute validator commission — valid but arbitrary; commission does not affect supply,
// claim resolution, or the allocation assertions, so a fixed value is fine for the pre-flight.
const (
	substituteCommissionRate          = "0.1"
	substituteCommissionMaxRate       = "0.2"
	substituteCommissionMaxChangeRate = "0.01"
)

// prepareBoot builds the bootable doctored genesis and stages it into K substitute node
// homes. The real validators are collapsed into K substitutes (default from the engine) so
// the boot cost is independent of network size: their self-delegations are partitioned to
// preserve the real total (keeping cfg.TotalSupply valid), and delegated claims are remapped
// onto the substitutes via a round-robin real→substitute moniker map. Returns the node homes
// (each holds a consensus key the engine controls) with the boot genesis written in, plus the
// Substitution record (substitutes + the delegation remap) for the verbose report.
func prepareBoot(ctx context.Context, in Input, p *prepared, k int) ([]string, *Substitution, error) {
	bin := chainBinary{in.BinaryPath}
	hrp := in.Config.AddressPrefix

	realMonikers, totalSelfDeleg, err := parseRealValidators(ctx, p.realGentxDir, hrp)
	if err != nil {
		return nil, nil, fmt.Errorf("parse real validators: %w", err)
	}
	k = clampValidators(k, totalSelfDeleg)
	amounts := partitionSelfDelegation(totalSelfDeleg, k)

	subGentxDir := filepath.Join(p.dir, "gentx-sub")
	if err := os.MkdirAll(subGentxDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create substitute gentx dir: %w", err)
	}
	subMonikers := make([]string, k)
	homes := make([]string, k)
	for i := range k {
		subMonikers[i] = fmt.Sprintf("rehearse-val-%d", i)
		out := filepath.Join(subGentxDir, fmt.Sprintf("gentx-%d.json", i))
		home, err := generateSubstituteValidator(ctx, bin, p.dir, in.Config, subMonikers[i], amounts[i], out)
		if err != nil {
			return nil, nil, err
		}
		homes[i] = home
	}

	// Round-robin map real validator monikers → substitutes, so delegated claims spread
	// across the K substitutes rather than piling onto one.
	remap := make(map[string]string, len(realMonikers))
	for i, m := range realMonikers {
		remap[m] = subMonikers[i%k]
	}

	bootRepos := p.repos
	if claimsCSV, ok := in.Allocations[AllocationClaims]; ok {
		bootClaims := filepath.Join(p.dir, "claims-boot.csv")
		if err := os.WriteFile(bootClaims, remapClaimDelegations(claimsCSV, remap), 0o600); err != nil {
			return nil, nil, fmt.Errorf("write remapped claims: %w", err)
		}
		bootRepos.Claims = gcsv.NewCSVClaimRepository(bootClaims, computeModuleAddresses(hrp, in.Config.ExtraModules))
	}
	bootRepos.Validators = gentx.NewValidatorRepository(subGentxDir, hrp)

	bootGenesis, err := genesis.Build(ctx, p.baseGenesis, in.Config, bootRepos)
	if err != nil {
		return nil, nil, fmt.Errorf("build boot genesis: %w", err)
	}
	for _, home := range homes {
		if err := bootGenesis.SaveAs(filepath.Join(home, "config", "genesis.json")); err != nil {
			return nil, nil, fmt.Errorf("stage boot genesis into %s: %w", home, err)
		}
	}

	sub := &Substitution{
		Validators:       make([]SubstituteValidator, k),
		RealToSubstitute: remap,
	}
	for i := range k {
		sub.Validators[i] = SubstituteValidator{Moniker: subMonikers[i], SelfDelegation: amounts[i]}
	}
	return homes, sub, nil
}

// parseRealValidators reads the materialized real gentxs and returns their monikers and the
// summed self-delegation (the value the substitutes must preserve for supply to reconcile).
func parseRealValidators(ctx context.Context, gentxDir, hrp string) (monikers []string, totalSelfDeleg int64, err error) {
	vals, err := gentx.NewValidatorRepository(gentxDir, hrp).GetValidators(ctx)
	if err != nil {
		return nil, 0, err
	}
	for i := range vals {
		monikers = append(monikers, vals[i].Name())
		totalSelfDeleg += vals[i].Amount()
	}
	return monikers, totalSelfDeleg, nil
}

// generateSubstituteValidator inits a fresh node home (→ a distinct consensus key), creates
// an operator key, funds it with selfDeleg, and produces a gentx written to gentxOut. The
// home retains its consensus key for the boot. Returns the home directory.
func generateSubstituteValidator(
	ctx context.Context, bin chainBinary, workdir string, cfg genesis.ChainConfig, moniker string, selfDeleg int64, gentxOut string,
) (string, error) {
	home := filepath.Join(workdir, "sub-"+moniker)
	keyring := []string{"--keyring-backend", "test", "--home", home}
	amount := fmt.Sprintf("%d%s", selfDeleg, cfg.BondDenom)

	if _, err := bin.run(ctx, "init", moniker, "--chain-id", cfg.ChainID, "--home", home); err != nil {
		return "", fmt.Errorf("init %s: %w", moniker, err)
	}
	if _, err := bin.run(ctx, append([]string{"keys", "add", "operator"}, keyring...)...); err != nil {
		return "", fmt.Errorf("keys add %s: %w", moniker, err)
	}
	addrOut, err := bin.run(ctx, append([]string{"keys", "show", "operator", "-a"}, keyring...)...)
	if err != nil {
		return "", fmt.Errorf("keys show %s: %w", moniker, err)
	}
	addr := strings.TrimSpace(string(addrOut))
	if _, err := bin.run(ctx, "genesis", "add-genesis-account", addr, amount, "--home", home); err != nil {
		return "", fmt.Errorf("add-genesis-account %s: %w", moniker, err)
	}
	gentxArgs := append([]string{
		"genesis", "gentx", "operator", amount,
		"--chain-id", cfg.ChainID, "--moniker", moniker,
		"--commission-rate", substituteCommissionRate,
		"--commission-max-rate", substituteCommissionMaxRate,
		"--commission-max-change-rate", substituteCommissionMaxChangeRate,
		"--output-document", gentxOut,
	}, keyring...)
	if _, err := bin.run(ctx, gentxArgs...); err != nil {
		return "", fmt.Errorf("gentx %s: %w", moniker, err)
	}
	return home, nil
}

// clampValidators returns the substitute count: at least 1, and no more than totalSelfDeleg
// when that is positive (so each substitute can take a positive self-delegation). A
// non-positive total is a degenerate input, left for the boot build to reject.
func clampValidators(k int, totalSelfDeleg int64) int {
	if k < 1 {
		return 1
	}
	if totalSelfDeleg >= 1 && int64(k) > totalSelfDeleg {
		return int(totalSelfDeleg)
	}
	return k
}

// partitionSelfDelegation splits total into k positive parts summing to total (remainder
// spread over the first parts). Requires k ≥ 1 and, for all parts to be positive, total ≥ k.
func partitionSelfDelegation(total int64, k int) []int64 {
	parts := make([]int64, k)
	base := total / int64(k)
	rem := total % int64(k)
	for i := range parts {
		parts[i] = base
		if int64(i) < rem {
			parts[i]++
		}
	}
	return parts
}

// remapClaimDelegations rewrites the delegate_to column (3rd field) of a claims CSV via the
// real→substitute moniker map, leaving address/amount untouched (so supply is unchanged).
// Rows with no delegation are passed through; an unparseable input is returned as-is so the
// genesis build surfaces the real error.
func remapClaimDelegations(data []byte, remap map[string]string) []byte {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	var out bytes.Buffer
	w := csv.NewWriter(&out)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return data
		}
		if len(rec) >= 3 {
			if sub, ok := remap[strings.TrimSpace(rec[2])]; ok {
				rec[2] = sub
			}
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return out.Bytes()
}
