package rehearse

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
)

// maxItemsPerCategory caps how many per-row checks (accounts, claims, grants, authz,
// feegrant) the suite runs. Large allocation sets (airdrops) would otherwise spawn one CLI
// query per address; the aggregate supply/bonded-pool checks still cover the whole set, and
// a capped category emits a disclosed SKIP step (never a silent truncation).
const maxItemsPerCategory = 50

// floatEpsilon is the tolerance for SDK decimal comparisons ("0.05" == "0.050000000000000000").
const floatEpsilon = 1e-9

// assertAll runs the input-derived assertion suite against the booted chain and returns one
// Step per check. It never aborts early, so the report shows every assertion. Expected values
// come from the Input (cfg + allocation CSVs); the chain runs SUBSTITUTE validators, so the
// validator/bonded checks are aggregate (count + preserved totals), never per-validator
// identity. A param the cfg leaves unset is SKIPped (gentool kept the genesis default).
func assertAll(ctx context.Context, in Input, sub *Substitution, rpcURL string) []Step {
	a := &asserter{
		ctx:   ctx,
		cfg:   in.Config,
		alloc: in.Allocations,
		sub:   sub,
		bin:   chainBinary{in.BinaryPath},
		node:  rpcURL,
		rpc:   newRPCClient(rpcURL),
	}
	a.chainID()
	a.stakingParams()
	a.slashingParams()
	a.govParams()
	a.mintParams()
	a.supply()
	a.bondedPool()
	a.communityPool()
	a.accounts()
	a.validators()
	a.vesting()
	a.authz()
	a.feegrant()
	a.denomMetadata()
	return a.steps
}

type asserter struct {
	ctx   context.Context
	cfg   genesis.ChainConfig
	alloc map[AllocationType][]byte
	sub   *Substitution
	bin   chainBinary
	node  string
	rpc   rpcClient
	steps []Step
}

// ── step recorders ────────────────────────────────────────────────────────────

func (a *asserter) pass(name, detail string) {
	a.steps = append(a.steps, Step{Name: "assert:" + name, Status: StepPass, Detail: detail})
}

func (a *asserter) fail(name, detail string) {
	a.steps = append(a.steps, Step{Name: "assert:" + name, Status: StepFail, Detail: detail})
}

func (a *asserter) skip(name, detail string) {
	a.steps = append(a.steps, Step{Name: "assert:" + name, Status: StepSkip, Detail: detail})
}

// eq records pass/fail on exact string equality.
func (a *asserter) eq(name, expected, actual string) {
	if expected == actual {
		a.pass(name, actual)
	} else {
		a.fail(name, fmt.Sprintf("expected %q, got %q", expected, actual))
	}
}

// eqFloat records pass/fail tolerating SDK decimal padding ("0.05" == "0.050000000000000000").
func (a *asserter) eqFloat(name, expected, actual string) {
	if floatEq(expected, actual) {
		a.pass(name, actual)
	} else {
		a.fail(name, fmt.Sprintf("expected ≈%s, got %q", expected, actual))
	}
}

// eqDurationSeconds records pass/fail comparing an SDK duration ("1814400s"/"504h0m0s") to
// an expected count of seconds.
func (a *asserter) eqDurationSeconds(name string, expected int64, actual string) {
	got, err := durationSeconds(actual)
	if err != nil {
		a.fail(name, fmt.Sprintf("unparseable duration %q: %v", actual, err))
		return
	}
	if got == expected {
		a.pass(name, fmt.Sprintf("%s (=%ds)", actual, got))
	} else {
		a.fail(name, fmt.Sprintf("expected %ds, got %q (=%ds)", expected, actual, got))
	}
}

// query runs `<bin> <args> --node <rpc> --output json`. On failure it records a fail step
// (with the binary's diagnostics) and returns ok=false.
func (a *asserter) query(name string, args ...string) ([]byte, bool) {
	full := make([]string, 0, len(args)+4)
	full = append(full, args...)
	full = append(full, "--node", a.node, "--output", "json")
	out, err := a.bin.run(a.ctx, full...)
	if err != nil {
		a.fail(name, "query error: "+strings.TrimSpace(string(out)))
		return nil, false
	}
	return out, true
}

// ── chain identity ──────────────────────────────────────────────────────────

func (a *asserter) chainID() {
	var out struct {
		Result struct {
			NodeInfo struct {
				Network string `json:"network"`
			} `json:"node_info"`
		} `json:"result"`
	}
	if err := a.rpc.getJSON(a.ctx, "/status", &out); err != nil {
		a.fail("chain_id", "query error: "+err.Error())
		return
	}
	a.eq("chain_id", a.cfg.ChainID, out.Result.NodeInfo.Network)
}

// ── module params (only what cfg pins down) ──────────────────────────────────

func (a *asserter) stakingParams() {
	out, ok := a.query("params:staking", "query", "staking", "params")
	if !ok {
		return
	}
	m := paramsMap(out)
	// bond_denom is always set (BondDenom is required input).
	a.eq("params:staking.bond_denom", a.cfg.BondDenom, jsonScalar(m["bond_denom"]))
	if a.cfg.UnbondingTimeSeconds > 0 {
		a.eqDurationSeconds("params:staking.unbonding_time", a.cfg.UnbondingTimeSeconds, jsonScalar(m["unbonding_time"]))
	}
	if a.cfg.MaxValidators > 0 {
		a.eq("params:staking.max_validators", strconv.FormatUint(uint64(a.cfg.MaxValidators), 10), jsonScalar(m["max_validators"]))
	}
	if a.cfg.MinCommissionRate != "" {
		a.eqFloat("params:staking.min_commission_rate", a.cfg.MinCommissionRate, jsonScalar(m["min_commission_rate"]))
	}
}

func (a *asserter) slashingParams() {
	out, ok := a.query("params:slashing", "query", "slashing", "params")
	if !ok {
		return
	}
	m := paramsMap(out)
	if a.cfg.SignedBlocksWindow > 0 {
		a.eq("params:slashing.signed_blocks_window", strconv.FormatInt(a.cfg.SignedBlocksWindow, 10), jsonScalar(m["signed_blocks_window"]))
	}
	if a.cfg.MinSignedPerWindow != "" {
		a.eqFloat("params:slashing.min_signed_per_window", a.cfg.MinSignedPerWindow, jsonScalar(m["min_signed_per_window"]))
	}
	if a.cfg.DowntimeJailDurationSeconds > 0 {
		a.eqDurationSeconds("params:slashing.downtime_jail_duration", a.cfg.DowntimeJailDurationSeconds, jsonScalar(m["downtime_jail_duration"]))
	}
	if a.cfg.SlashFractionDoubleSign != "" {
		a.eqFloat("params:slashing.slash_fraction_double_sign", a.cfg.SlashFractionDoubleSign, jsonScalar(m["slash_fraction_double_sign"]))
	}
	if a.cfg.SlashFractionDowntime != "" {
		a.eqFloat("params:slashing.slash_fraction_downtime", a.cfg.SlashFractionDowntime, jsonScalar(m["slash_fraction_downtime"]))
	}
}

func (a *asserter) govParams() {
	out, ok := a.query("params:gov", "query", "gov", "params")
	if !ok {
		return
	}
	// gov v1 nests under .params; legacy under .voting_params/.deposit_params.
	var top map[string]json.RawMessage
	_ = json.Unmarshal(out, &top)
	m := paramsMap(out)
	if a.cfg.GovVotingPeriod != "" {
		vp := jsonScalar(m["voting_period"])
		if vp == "" {
			vp = scalarFromSub(top["voting_params"], "voting_period")
		}
		// cfg stores it as an SDK duration string ("172800s"); compare second-for-second.
		if want, err := durationSeconds(a.cfg.GovVotingPeriod); err == nil {
			a.eqDurationSeconds("params:gov.voting_period", want, vp)
		}
	}
	if a.cfg.GovMinDepositAmount > 0 {
		dep := m["min_deposit"]
		if dep == nil {
			dep = subField(top["deposit_params"], "min_deposit")
		}
		a.eq("params:gov.min_deposit", strconv.FormatInt(a.cfg.GovMinDepositAmount, 10), coinAmount(dep, a.cfg.BondDenom))
	}
}

func (a *asserter) mintParams() {
	out, ok := a.query("params:mint", "query", "mint", "params")
	if !ok {
		return
	}
	m := paramsMap(out)
	// mint_denom is the bond denom (gentool always sets it).
	a.eq("params:mint.mint_denom", a.cfg.BondDenom, jsonScalar(m["mint_denom"]))
	if a.cfg.BlocksPerYear > 0 {
		a.eq("params:mint.blocks_per_year", strconv.FormatInt(a.cfg.BlocksPerYear, 10), jsonScalar(m["blocks_per_year"]))
	}
}

// ── supply, pool, balances ───────────────────────────────────────────────────

func (a *asserter) supply() {
	out, ok := a.query("supply_reconciles", "query", "bank", "total")
	if !ok {
		return
	}
	var resp struct {
		Supply []struct{ Denom, Amount string } `json:"supply"`
	}
	_ = json.Unmarshal(out, &resp)
	got := ""
	for _, c := range resp.Supply {
		if c.Denom == a.cfg.BondDenom {
			got = c.Amount
		}
	}
	a.eq("supply_reconciles", strconv.FormatInt(a.cfg.TotalSupply, 10), got)
}

func (a *asserter) bondedPool() {
	out, ok := a.query("bonded_pool", "query", "staking", "pool")
	if !ok {
		return
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(out, &top)
	pool := top
	if p, has := top["pool"]; has {
		var m map[string]json.RawMessage
		if json.Unmarshal(p, &m) == nil {
			pool = m
		}
	}
	a.eq("bonded_pool", strconv.FormatInt(a.expectedBondedTokens(), 10), jsonScalar(pool["bonded_tokens"]))
}

// expectedBondedTokens = Σ substitute self-delegations (preserved from the real set) plus the
// bonded portion (amount − reserve) of every delegating claim. Both are unchanged by the
// substitution, so this reconciles against the booted chain.
func (a *asserter) expectedBondedTokens() int64 {
	var total int64
	for _, v := range a.sub.Validators {
		total += v.SelfDelegation
	}
	reserve := a.cfg.NonStaked()
	for _, row := range csvRows(a.alloc[AllocationClaims]) {
		if len(row) >= 3 && strings.TrimSpace(row[2]) != "" {
			if amt, err := strconv.ParseInt(strings.TrimSpace(row[1]), 10, 64); err == nil {
				total += amt - reserve
			}
		}
	}
	return total
}

func (a *asserter) communityPool() {
	if a.cfg.CommunityPoolAmount == 0 {
		a.skip("community_pool", "no community pool seeded (cfg.CommunityPoolAmount == 0)")
		return
	}
	out, ok := a.query("community_pool", "query", "distribution", "community-pool")
	if !ok {
		return
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(out, &top)
	pool := top["pool"]
	if pool == nil {
		pool = top["community_pool"]
	}
	a.eqFloat("community_pool", strconv.FormatInt(a.cfg.CommunityPoolAmount, 10), coinAmount(pool, a.cfg.BondDenom))
}

func (a *asserter) accounts() {
	a.eachRow("accounts", a.alloc[AllocationAccounts], func(row []string) {
		if len(row) < 2 {
			return
		}
		addr := strings.TrimSpace(row[0])
		a.eq("account_balance:"+addr, strings.TrimSpace(row[1]), a.bankBalance(addr))
	})
}

// bankBalance returns the bond-denom liquid balance of addr ("" on query/parse failure).
func (a *asserter) bankBalance(addr string) string {
	out, err := a.bin.run(a.ctx, "query", "bank", "balances", addr, "--node", a.node, "--output", "json")
	if err != nil {
		return ""
	}
	var resp struct {
		Balances []struct{ Denom, Amount string } `json:"balances"`
	}
	_ = json.Unmarshal(out, &resp)
	for _, c := range resp.Balances {
		if c.Denom == a.cfg.BondDenom {
			return c.Amount
		}
	}
	return "0"
}

// ── validators (aggregate only — substitute identity) ────────────────────────

func (a *asserter) validators() {
	out, ok := a.query("validators_bonded", "query", "staking", "validators")
	if !ok {
		return
	}
	var resp struct {
		Validators []struct {
			Status string `json:"status"`
		} `json:"validators"`
	}
	_ = json.Unmarshal(out, &resp)
	bonded := 0
	for _, v := range resp.Validators {
		if v.Status == "BOND_STATUS_BONDED" {
			bonded++
		}
	}
	a.eq("validators_bonded", strconv.Itoa(len(a.sub.Validators)), strconv.Itoa(bonded))
}

// ── vesting accounts (claims → delayed, grants → continuous) ──────────────────

func (a *asserter) vesting() {
	reserve := a.cfg.NonStaked()
	a.eachRow("claims", a.alloc[AllocationClaims], func(row []string) {
		if len(row) < 2 {
			return
		}
		addr, amount := strings.TrimSpace(row[0]), strings.TrimSpace(row[1])
		delegated := len(row) >= 3 && strings.TrimSpace(row[2]) != ""
		a.vestingAccount("claim", addr, amount, "DelayedVestingAccount")
		if delegated {
			a.delegatingClaim(addr, amount, reserve)
		} else {
			a.eq("claim_liquid:"+addr, amount, a.bankBalance(addr))
		}
	})
	a.eachRow("grants", a.alloc[AllocationGrants], func(row []string) {
		if len(row) < 2 {
			return
		}
		addr, amount := strings.TrimSpace(row[0]), strings.TrimSpace(row[1])
		a.vestingAccount("grant", addr, amount, "ContinuousVestingAccount")
		a.eq("grant_liquid:"+addr, amount, a.bankBalance(addr))
		if a.cfg.GrantsVestingStart > 0 {
			a.eq("grant_start:"+addr, strconv.FormatInt(a.cfg.GrantsVestingStart, 10), a.accountStartTime(addr))
		}
	})
}

// vestingAccount checks the account exists with the expected vesting type and original_vesting.
func (a *asserter) vestingAccount(kind, addr, amount, wantType string) {
	out, err := a.bin.run(a.ctx, "query", "auth", "account", addr, "--node", a.node, "--output", "json")
	if err != nil {
		a.fail(kind+"_account:"+addr, "query error: "+strings.TrimSpace(string(out)))
		return
	}
	inner, typ := unwrapAccount(out)
	if !strings.Contains(typ, wantType) {
		a.fail(kind+"_type:"+addr, fmt.Sprintf("expected type containing %q, got %q", wantType, typ))
	} else {
		a.pass(kind+"_type:"+addr, typ)
	}
	bva := baseVestingAccount(inner)
	a.eq(kind+"_original_vesting:"+addr, amount, coinAmount(bva["original_vesting"], a.cfg.BondDenom))
}

// delegatingClaim checks a delegated claim's split: liquid == reserve, delegated_vesting and a
// single delegation each == amount − reserve.
func (a *asserter) delegatingClaim(addr, amount string, reserve int64) {
	amt, err := strconv.ParseInt(amount, 10, 64)
	if err != nil {
		a.fail("claim_split:"+addr, "unparseable amount "+amount)
		return
	}
	bonded := strconv.FormatInt(amt-reserve, 10)
	a.eq("claim_liquid:"+addr, strconv.FormatInt(reserve, 10), a.bankBalance(addr))

	out, err := a.bin.run(a.ctx, "query", "auth", "account", addr, "--node", a.node, "--output", "json")
	if err == nil {
		inner, _ := unwrapAccount(out)
		a.eq("claim_delegated_vesting:"+addr, bonded, coinAmount(baseVestingAccount(inner)["delegated_vesting"], a.cfg.BondDenom))
	}

	del, err := a.bin.run(a.ctx, "query", "staking", "delegations", addr, "--node", a.node, "--output", "json")
	if err != nil {
		a.fail("claim_delegation:"+addr, "query error: "+strings.TrimSpace(string(del)))
		return
	}
	var resp struct {
		DelegationResponses []struct {
			Delegation struct {
				Shares string `json:"shares"`
			} `json:"delegation"`
		} `json:"delegation_responses"`
	}
	_ = json.Unmarshal(del, &resp)
	if len(resp.DelegationResponses) != 1 {
		a.fail("claim_delegation_count:"+addr, fmt.Sprintf("expected 1 delegation, got %d", len(resp.DelegationResponses)))
		return
	}
	a.pass("claim_delegation_count:"+addr, "1")
	a.eqFloat("claim_delegation_shares:"+addr, bonded, resp.DelegationResponses[0].Delegation.Shares)
}

// accountStartTime returns a continuous-vesting account's start_time ("" on failure).
func (a *asserter) accountStartTime(addr string) string {
	out, err := a.bin.run(a.ctx, "query", "auth", "account", addr, "--node", a.node, "--output", "json")
	if err != nil {
		return ""
	}
	inner, _ := unwrapAccount(out)
	if s := jsonScalar(inner["start_time"]); s != "" {
		return s
	}
	return scalarFromSub(inner["value"], "start_time")
}

// ── authz / feegrant ─────────────────────────────────────────────────────────

func (a *asserter) authz() {
	a.eachRow("authz", a.alloc[AllocationAuthz], func(row []string) {
		if len(row) < 2 {
			return
		}
		granter, grantee := strings.TrimSpace(row[0]), strings.TrimSpace(row[1])
		out, ok := a.query("authz_grant:"+grantee, "query", "authz", "grants-by-grantee", grantee)
		if !ok {
			return
		}
		var resp struct {
			Grants []struct {
				Granter       string          `json:"granter"`
				Authorization json.RawMessage `json:"authorization"`
			} `json:"grants"`
		}
		_ = json.Unmarshal(out, &resp)
		if len(resp.Grants) == 0 {
			a.fail("authz_grant:"+grantee, "no grants found")
			return
		}
		a.eq("authz_granter:"+grantee, granter, resp.Grants[0].Granter)
		a.contains("authz_type:"+grantee, "GenericAuthorization", typeOf(resp.Grants[0].Authorization))
	})
}

func (a *asserter) feegrant() {
	a.eachRow("feegrant", a.alloc[AllocationFeegrant], func(row []string) {
		if len(row) < 3 {
			return
		}
		granter, grantee, limit := strings.TrimSpace(row[0]), strings.TrimSpace(row[1]), strings.TrimSpace(row[2])
		out, ok := a.query("feegrant:"+granter, "query", "feegrant", "grants-by-granter", granter)
		if !ok {
			return
		}
		var resp struct {
			Allowances []struct {
				Grantee   string          `json:"grantee"`
				Allowance json.RawMessage `json:"allowance"`
			} `json:"allowances"`
		}
		_ = json.Unmarshal(out, &resp)
		if len(resp.Allowances) == 0 {
			a.fail("feegrant:"+granter, "no allowances found")
			return
		}
		a.eq("feegrant_grantee:"+granter, grantee, resp.Allowances[0].Grantee)
		a.contains("feegrant_type:"+granter, "BasicAllowance", typeOf(resp.Allowances[0].Allowance))
		a.eq("feegrant_spend_limit:"+granter, limit, allowanceSpendLimit(resp.Allowances[0].Allowance, a.cfg.BondDenom))
	})
}

// ── denom metadata ───────────────────────────────────────────────────────────

func (a *asserter) denomMetadata() {
	if a.cfg.DenomBase == "" {
		a.skip("denom_metadata", "no denom metadata configured (cfg.DenomBase empty)")
		return
	}
	out, ok := a.query("denom_metadata", "query", "bank", "denom-metadata", a.cfg.DenomBase)
	if !ok {
		return
	}
	var top map[string]json.RawMessage
	_ = json.Unmarshal(out, &top)
	meta := top
	if m, has := top["metadata"]; has {
		var mm map[string]json.RawMessage
		if json.Unmarshal(m, &mm) == nil {
			meta = mm
		}
	}
	a.eq("denom_metadata.base", a.cfg.DenomBase, jsonScalar(meta["base"]))
	if a.cfg.DenomDisplay != "" {
		a.eq("denom_metadata.display", a.cfg.DenomDisplay, jsonScalar(meta["display"]))
	}
	a.eq("denom_metadata.exponent", strconv.FormatUint(uint64(a.cfg.DenomExponent), 10), maxExponent(meta["denom_units"]))
}

// contains records pass/fail on substring match (proto "/cosmos…GenericAuthorization" or
// amino "cosmos-sdk/GenericAuthorization").
func (a *asserter) contains(name, needle, actual string) {
	if strings.Contains(actual, needle) {
		a.pass(name, actual)
	} else {
		a.fail(name, fmt.Sprintf("expected to contain %q, got %q", needle, actual))
	}
}

// eachRow runs fn for each CSV row up to maxItemsPerCategory; a capped category emits a
// disclosed SKIP step so the truncation is never silent.
func (a *asserter) eachRow(cat string, data []byte, fn func(row []string)) {
	rows := csvRows(data)
	limit := len(rows)
	if limit > maxItemsPerCategory {
		limit = maxItemsPerCategory
	}
	for i := range limit {
		if len(rows[i]) > 0 {
			fn(rows[i])
		}
	}
	if len(rows) > limit {
		a.skip(cat+"_capped", fmt.Sprintf(
			"checked %d of %d %s rows (cap %d); aggregate checks cover the rest",
			limit, len(rows), cat, maxItemsPerCategory))
	}
}

// ── pure helpers ──────────────────────────────────────────────────────────────

// csvRows parses opaque CSV bytes into rows (variable field counts tolerated).
func csvRows(data []byte) [][]string {
	if len(data) == 0 {
		return nil
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil
	}
	return rows
}

// jsonScalar renders a scalar RawMessage as a bare string, stripping surrounding quotes so a
// proto-JSON number (100) and an amino string ("100") both yield "100".
func jsonScalar(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var unq string
		if json.Unmarshal(raw, &unq) == nil {
			return unq
		}
	}
	if s == "null" {
		return ""
	}
	return s
}

// paramsMap returns the params object, unwrapping the {"params": {...}} envelope when present.
func paramsMap(raw []byte) map[string]json.RawMessage {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return nil
	}
	if p, ok := top["params"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(p, &m) == nil {
			return m
		}
	}
	return top
}

// subField returns field from a nested raw object ({} when absent).
func subField(raw json.RawMessage, field string) json.RawMessage {
	if raw == nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m[field]
}

// scalarFromSub returns a scalar field nested inside raw.
func scalarFromSub(raw json.RawMessage, field string) string {
	return jsonScalar(subField(raw, field))
}

// floatEq compares two decimal strings within floatEpsilon.
func floatEq(a, b string) bool {
	af, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	bf, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if err1 != nil || err2 != nil {
		return false
	}
	return math.Abs(af-bf) < floatEpsilon
}

// durationSeconds parses an SDK duration ("1814400s", "504h0m0s", "600s") to whole seconds.
func durationSeconds(s string) (int64, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return int64(d.Seconds()), nil
}

// coinAmount returns the amount for denom from a coins array RawMessage ("" when absent).
func coinAmount(raw json.RawMessage, denom string) string {
	if raw == nil {
		return ""
	}
	var coins []struct{ Denom, Amount string }
	if json.Unmarshal(raw, &coins) != nil {
		return ""
	}
	for _, c := range coins {
		if c.Denom == denom {
			return c.Amount
		}
	}
	return ""
}

// unwrapAccount handles the SDK v0.50 gRPC ({"account": {...}}) wrapper and returns the inner
// account object plus its type (proto "@type" or amino "type").
func unwrapAccount(raw []byte) (inner map[string]json.RawMessage, accType string) {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return nil, ""
	}
	inner = top
	if acc, ok := top["account"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(acc, &m) == nil {
			inner = m
		}
	}
	accType = jsonScalar(inner["@type"])
	if accType == "" {
		accType = jsonScalar(inner["type"])
	}
	return inner, accType
}

// baseVestingAccount returns the base_vesting_account object, trying the proto-JSON top level
// and the amino .value nesting.
func baseVestingAccount(inner map[string]json.RawMessage) map[string]json.RawMessage {
	if raw, ok := inner["base_vesting_account"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) == nil {
			return m
		}
	}
	if val, ok := inner["value"]; ok {
		if raw := subField(val, "base_vesting_account"); raw != nil {
			var m map[string]json.RawMessage
			if json.Unmarshal(raw, &m) == nil {
				return m
			}
		}
	}
	return map[string]json.RawMessage{}
}

// typeOf returns the proto "@type" or amino "type" of a wrapped message.
func typeOf(raw json.RawMessage) string {
	t := scalarFromSub(raw, "@type")
	if t == "" {
		t = scalarFromSub(raw, "type")
	}
	return t
}

// allowanceSpendLimit returns the spend_limit amount for denom from a feegrant allowance,
// trying proto-JSON (top-level spend_limit) and amino (.value.spend_limit).
func allowanceSpendLimit(raw json.RawMessage, denom string) string {
	if amt := coinAmount(subField(raw, "spend_limit"), denom); amt != "" {
		return amt
	}
	return coinAmount(subField(subField(raw, "value"), "spend_limit"), denom)
}

// maxExponent returns the largest positive exponent across a denom_units array, as a string.
func maxExponent(raw json.RawMessage) string {
	var units []struct {
		Exponent int `json:"exponent"`
	}
	if json.Unmarshal(raw, &units) != nil {
		return ""
	}
	maxExp := 0
	for _, u := range units {
		if u.Exponent > maxExp {
			maxExp = u.Exponent
		}
	}
	return strconv.Itoa(maxExp)
}
