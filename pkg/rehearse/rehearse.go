// Package rehearse boots an ephemeral chain from a candidate genesis input and runs
// the on-chain assertion suite, returning a structured pass/fail. It is the engine
// behind both the standalone rehearse CLI/Action and the coordd-connected daemon —
// it knows nothing about coordd.
//
// What a PASS certifies: the real input set assembles (build), and a representative
// chain initializes and advances (boot on SUBSTITUTE validators, since the real
// gentxs consensus private keys are unavailable). It is a pre-flight on the
// input set, not a certification that the real network produces blocks, and it emits
// no publishable genesis.
package rehearse

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
)

// AllocationType identifies a curated allocation file (CSV — gentool's format).
type AllocationType string

const (
	AllocationAccounts AllocationType = "accounts"
	AllocationClaims   AllocationType = "claims"
	AllocationGrants   AllocationType = "grants"
	AllocationAuthz    AllocationType = "authz"
	AllocationFeegrant AllocationType = "feegrant"
)

// Input is the complete, coordd-agnostic input to a rehearsal run.
type Input struct {
	// Config is the full genesis ChainConfig (chain id, denom, total supply, vesting
	// windows, params…). The standalone runner reads it from a gentool-style config; the
	// coordd client maps coordd's chain record onto it (filling supply etc.).
	Config genesis.ChainConfig

	// Allocations holds each curated allocation file as raw CSV bytes (gentool's format).
	// Absent keys are skipped (the corresponding genesis.Repositories field stays nil).
	Allocations map[AllocationType][]byte

	// Gentxs holds the approved gentxs as raw JSON, one per validator. These are the REAL
	// validators: used for the build (assembly) check only — never booted (no priv keys).
	Gentxs [][]byte

	// BinaryPath is the chaind binary used for `init`, substitute-gentx generation, and
	// boot. BinarySHA256 (hex) is verified against it before anything runs.
	BinaryPath   string
	BinarySHA256 string
}

// Outcome mirrors the bridge contract's tri-state.
type Outcome string

const (
	OutcomePass  Outcome = "PASS"  // built, booted, all assertions passed
	OutcomeFail  Outcome = "FAIL"  // a real negative verdict — build/boot/assertion failed
	OutcomeError Outcome = "ERROR" // infra failure (no runtime, binary mismatch, timeout)
)

// StepStatus is a per-step verdict.
type StepStatus string

const (
	StepPass StepStatus = "PASS"
	StepFail StepStatus = "FAIL"
	StepSkip StepStatus = "SKIP"
)

// Step is one stage or assertion result; maps onto the result fact's steps[].
type Step struct {
	Name   string     // "build", "boot", "assert:supply_reconciles", …
	Status StepStatus //
	Detail string     // human detail (error text on failure)
}

// Result is the structured outcome of a run; the coordd daemon maps it onto the signed fact,
// and Report renders it verbose.
type Result struct {
	Outcome    Outcome
	FailedStep string // "" on PASS
	Summary    string // one-line verdict
	Steps      []Step
	Validators int // substitute validators that booted

	// Substitution records how the real validator set was collapsed onto the boot validators
	// (nil until the boot genesis is prepared). Drives the verbose report's remap explanation.
	Substitution *Substitution

	// Err is the wrapped sentinel for a FAIL/ERROR run (nil on PASS). Not serialized — it is
	// the controlled handle for callers to branch with errors.Is (e.g. ErrInfrastructure).
	Err error

	StartedAt  time.Time
	FinishedAt time.Time
}

// SubstituteValidator is one throwaway validator the engine generated and booted.
type SubstituteValidator struct {
	Moniker        string
	SelfDelegation int64
}

// Substitution records the collapse of the real validator set onto the boot validators:
// the K substitutes that booted and, per real validator, which substitute its delegated
// claims were remapped onto (so the report can say "delegations to X23 → rehearse-val-1").
type Substitution struct {
	Validators       []SubstituteValidator
	RealToSubstitute map[string]string // real validator moniker → substitute moniker
}

// Runtime boots a prepared node set and exposes an RPC endpoint, then tears it down.
// v1: a local process-based impl (mirrors smoke.sh); a container-per-chain impl is a
// later drop-in. The engine owns the workdir; the Runtime owns the running processes.
type Runtime interface {
	// Boot starts the chain from the prepared node homes and returns a handle to it (or
	// errors). Readiness (RPC reachable, height advancing) is the caller's to poll via the
	// returned RPCURL. The caller MUST tear down the returned Booted.
	Boot(ctx context.Context, homes []string) (Booted, error)
}

// Booted is a running ephemeral chain.
type Booted interface {
	RPCURL() string
	Teardown() error
}

// Engine runs rehearsals. Construct with New.
type Engine struct {
	runtime    Runtime
	validators int           // substitute validators to generate and boot (default 2)
	bootWait   time.Duration // max wait for the first block
}

// Option configures an Engine.
type Option func(*Engine)

// WithValidators sets the substitute validator count (default 2).
func WithValidators(n int) Option { return func(e *Engine) { e.validators = n } }

// WithBootWait sets the max wait for the chain's first block (default 90s).
func WithBootWait(d time.Duration) Option { return func(e *Engine) { e.bootWait = d } }

// New returns an Engine that boots via the given Runtime.
func New(rt Runtime, opts ...Option) *Engine {
	e := &Engine{runtime: rt, validators: 2, bootWait: 90 * time.Second}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Run executes a full rehearsal: verify the binary, build the genesis from the real inputs
// (assembly check), build a bootable doctored genesis on substitute validators, boot it, run
// the assertion suite, and always tear down. It never returns a nil *Result for a completed
// run — outcome/steps carry the verdict; the error is reserved for the engine itself failing.
func (e *Engine) Run(ctx context.Context, in Input) (*Result, error) {
	res := &Result{Validators: e.validators, StartedAt: time.Now().UTC()}
	defer func() { res.FinishedAt = time.Now().UTC() }()

	// 1. Verify the chain binary matches the input's sha256 (D-e). Missing/mismatch = ERROR
	//    (an infra fault, not a verdict on the genesis).
	if err := verifyBinary(in.BinaryPath, in.BinarySHA256); err != nil {
		return failResult(res, OutcomeError, "verify_binary", fmt.Errorf("%w: %w", ErrBinaryVerification, err)), nil
	}
	res.Steps = append(res.Steps, Step{Name: "verify_binary", Status: StepPass})

	// 2. Build check on the REAL inputs. accounts is required (gentool needs ≥1 account, the
	//    supply anchor); a materialize failure (temp dir / `<chaind> init`) is infra → ERROR,
	//    while a build failure (the approved set doesn't assemble) is a real verdict → FAIL.
	if _, ok := in.Allocations[AllocationAccounts]; !ok {
		return failResult(res, OutcomeFail, "build", fmt.Errorf("%w: accounts allocation is required", ErrInvalidInput)), nil
	}
	if len(in.Gentxs) == 0 {
		return failResult(res, OutcomeFail, "build", fmt.Errorf("%w: at least one gentx is required", ErrInvalidInput)), nil
	}
	prep, err := materialize(ctx, in)
	if err != nil {
		return failResult(res, OutcomeError, "materialize", fmt.Errorf("%w: %w", ErrInfrastructure, err)), nil
	}
	defer func() { _ = os.RemoveAll(prep.dir) }()
	if err := buildCheck(ctx, in, prep); err != nil {
		return failResult(res, OutcomeFail, "build", fmt.Errorf("%w: %w", ErrGenesisBuild, err)), nil
	}
	res.Steps = append(res.Steps, Step{Name: "build", Status: StepPass})

	// 3. Collapse the real validators into e.validators substitute validators (fresh keys,
	//    self-delegation total preserved, delegated claims remapped) and stage the bootable
	//    genesis into their node homes. Failures here (keygen/gentx/build) are infra → ERROR.
	homes, sub, err := prepareBoot(ctx, in, prep, e.validators)
	if err != nil {
		return failResult(res, OutcomeError, "prepare_boot", fmt.Errorf("%w: %w", ErrInfrastructure, err)), nil
	}
	res.Substitution = sub
	res.Validators = len(homes)
	res.Steps = append(res.Steps, Step{Name: "prepare_boot", Status: StepPass})

	// 4. Boot the substitute validators (process-based runtime) and poll height ≥ 1 within
	//    e.bootWait; the chain is ALWAYS torn down. The substitute set is engine-built and
	//    fully online, so a boot/liveness failure is infra, not a genesis verdict → ERROR.
	booted, err := e.runtime.Boot(ctx, homes)
	if err != nil {
		return failResult(res, OutcomeError, "boot", fmt.Errorf("%w: %w", ErrBoot, err)), nil
	}
	defer func() { _ = booted.Teardown() }()
	if err := waitForHeight(ctx, booted.RPCURL(), 1, e.bootWait); err != nil {
		return failResult(res, OutcomeError, "boot", fmt.Errorf("%w: %w", ErrBoot, err)), nil
	}
	res.Steps = append(res.Steps, Step{Name: "boot", Status: StepPass})

	// 5. Run the input-derived assertion suite against the booted chain (one Step each, full
	//    smoke.sh parity). Any failed assertion is a real negative verdict → FAIL.
	res.Steps = append(res.Steps, assertAll(ctx, in, sub, booted.RPCURL())...)
	if failed := failedAssertions(res.Steps); len(failed) > 0 {
		res.Outcome = OutcomeFail
		res.FailedStep = failed[0]
		res.Err = fmt.Errorf("%w: %d assertion(s) failed", ErrAssertion, len(failed))
		res.Summary = fmt.Sprintf("%d assertion(s) failed (first: %s)", len(failed), failed[0])
		return res, nil
	}
	res.Outcome = OutcomePass
	res.Summary = fmt.Sprintf("built, booted %d substitute validators, all assertions passed", res.Validators)
	return res, nil
}

// failedAssertions returns the names of every failed Step (after build+boot pass, only
// assertion steps remain to fail).
func failedAssertions(steps []Step) []string {
	var failed []string
	for _, s := range steps {
		if s.Status == StepFail {
			failed = append(failed, s.Name)
		}
	}
	return failed
}

// failResult records a terminal failure on res and returns it for the caller to return.
// err should wrap a sentinel (errors.go); it is surfaced on res.Err for errors.Is branching.
func failResult(res *Result, outcome Outcome, step string, err error) *Result {
	res.Steps = append(res.Steps, Step{Name: step, Status: StepFail, Detail: err.Error()})
	res.Outcome = outcome
	res.FailedStep = step
	res.Summary = fmt.Sprintf("%s: %v", step, err)
	res.Err = err
	return res
}

// Report renders a verbose, human-readable account of the run: the outcome, duration, the
// validator substitution (including which boot validator each real validator's delegations
// were remapped onto), and every check's result. Suitable for CLI output or daemon logs.
func (r *Result) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rehearsal: %s\n", r.Outcome)
	if r.Summary != "" {
		fmt.Fprintf(&b, "Summary:   %s\n", r.Summary)
	}
	if !r.StartedAt.IsZero() && !r.FinishedAt.IsZero() {
		fmt.Fprintf(&b, "Duration:  %s\n", r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond))
	}

	if r.Substitution != nil {
		fmt.Fprintf(&b, "\nSubstitute validators (%d):\n", len(r.Substitution.Validators))
		for _, v := range r.Substitution.Validators {
			fmt.Fprintf(&b, "  %s — self-delegation %d\n", v.Moniker, v.SelfDelegation)
		}
		if len(r.Substitution.RealToSubstitute) > 0 {
			reals := make([]string, 0, len(r.Substitution.RealToSubstitute))
			for real := range r.Substitution.RealToSubstitute {
				reals = append(reals, real)
			}
			sort.Strings(reals)
			b.WriteString("Delegation remap (real validator → boot validator):\n")
			for _, real := range reals {
				fmt.Fprintf(&b, "  %s → %s\n", real, r.Substitution.RealToSubstitute[real])
			}
		}
	}

	b.WriteString("\nChecks:\n")
	for _, s := range r.Steps {
		glyph := "?"
		switch s.Status {
		case StepPass:
			glyph = "✓"
		case StepFail:
			glyph = "✗"
		case StepSkip:
			glyph = "–"
		}
		fmt.Fprintf(&b, "  %s %s", glyph, s.Name)
		if s.Detail != "" {
			fmt.Fprintf(&b, ": %s", s.Detail)
		}
		b.WriteByte('\n')
	}
	return b.String()
}
