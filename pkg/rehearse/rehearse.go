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

// Result is the structured outcome of a run; the coordd daemon maps it onto the signed fact.
type Result struct {
	Outcome    Outcome
	FailedStep string // "" on PASS
	Summary    string // one-line verdict
	Steps      []Step
	Validators int // substitute validators that booted
	StartedAt  time.Time
	FinishedAt time.Time
}

// Runtime boots a prepared node set and exposes an RPC endpoint, then tears it down.
// v1: a local process-based impl (mirrors smoke.sh); a container-per-chain impl is a
// later drop-in. The engine owns the workdir; the Runtime owns the running processes.
type Runtime interface {
	// Boot starts the chain from the prepared node homes and returns once an RPC endpoint
	// is reachable (or errors). The caller MUST tear down the returned Booted.
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
		return failResult(res, OutcomeError, "verify_binary", err), nil
	}
	res.Steps = append(res.Steps, Step{Name: "verify_binary", Status: StepPass})

	// 2. buildCheck: materialize input → temp workdir (allocation CSVs + gentx JSONs),
	//    `<chaind> init` a baseline, then genesis.Build(real gentxs + allocations). Failure =
	//    FAIL: build. Reuses csv.NewCSV*Repository + gentx.NewValidatorRepository.   (build.go)
	// 3. prepareBoot: generate e.validators substitute validators (fresh keys) and Build a
	//    bootable doctored genesis — real allocations, substitute gentxs.        (substitute.go)
	// 4. e.runtime.Boot(...) → poll height ≥ 1 within e.bootWait; ALWAYS Teardown.
	//                                                                       (runtime_process.go)
	// 5. assertAll: input-derived assertion suite vs the RPC endpoint, one Step each — full
	//    parity with smoke.sh's catalog (D-j).                                      (assert.go)
	_ = ctx
	return res, nil
}

// failResult records a terminal failure on res and returns it for the caller to return.
func failResult(res *Result, outcome Outcome, step string, err error) *Result {
	res.Steps = append(res.Steps, Step{Name: step, Status: StepFail, Detail: err.Error()})
	res.Outcome = outcome
	res.FailedStep = step
	res.Summary = fmt.Sprintf("%s: %v", step, err)
	return res
}
