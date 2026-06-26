package rehearse

import "errors"

// Sentinel errors for the failure categories, so callers (the standalone runner and the
// coordd daemon) branch with errors.Is rather than matching strings. Each is wrapped (%w)
// at the failure site and surfaced on Result.Err; Result.Outcome carries the coarse
// PASS/FAIL/ERROR verdict, these the precise cause.
var (
	// ErrBinaryVerification — the chain binary is missing or its sha256 does not match. ERROR.
	ErrBinaryVerification = errors.New("chain binary verification failed")

	// ErrInvalidInput — the input set is incomplete (e.g. no accounts, no validators). FAIL.
	ErrInvalidInput = errors.New("invalid rehearsal input")

	// ErrGenesisBuild — the approved input set does not assemble into a valid genesis. FAIL.
	ErrGenesisBuild = errors.New("genesis build failed")

	// ErrInfrastructure — an infrastructure fault (temp dir, `<chaind> init`, keygen, substitute
	// gentx, the boot genesis build, the runtime) prevented producing a verdict. ERROR.
	ErrInfrastructure = errors.New("rehearsal infrastructure failure")

	// ErrBoot — the substituted chain did not boot or advance a block in time. See Result.Outcome.
	ErrBoot = errors.New("chain boot failed")

	// ErrAssertion — an on-chain assertion failed. FAIL.
	ErrAssertion = errors.New("on-chain assertion failed")
)
