package rehearse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A binary that fails sha256 verification is an ERROR outcome (infra fault), reported
// at the verify_binary step — and the engine returns before touching the Runtime, so a
// nil Runtime is fine here.
func TestRun_BinaryMismatch_IsError(t *testing.T) {
	path := writeTempBinary(t, []byte("actual"))
	e := New(nil)

	res, err := e.Run(context.Background(), Input{
		BinaryPath:   path,
		BinarySHA256: sha256Hex([]byte("different")),
	})

	require.NoError(t, err)
	assert.Equal(t, OutcomeError, res.Outcome)
	assert.Equal(t, "verify_binary", res.FailedStep)
	require.ErrorIs(t, res.Err, ErrBinaryVerification)
	require.Len(t, res.Steps, 1)
	assert.Equal(t, StepFail, res.Steps[0].Status)
}

func TestResult_Report(t *testing.T) {
	r := &Result{
		Outcome: OutcomePass,
		Summary: "all checks passed",
		Steps: []Step{
			{Name: "build", Status: StepPass},
			{Name: "assert:supply_reconciles", Status: StepPass},
			{Name: "assert:denom_metadata", Status: StepSkip, Detail: "no denom metadata configured"},
		},
		Substitution: &Substitution{
			Validators:       []SubstituteValidator{{Moniker: "rehearse-val-0", SelfDelegation: 1000}},
			RealToSubstitute: map[string]string{"X23": "rehearse-val-0"},
		},
	}

	out := r.Report()
	assert.Contains(t, out, "PASS")
	assert.Contains(t, out, "rehearse-val-0 — self-delegation 1000")
	assert.Contains(t, out, "X23 → rehearse-val-0") // the remap explanation the report must show
	assert.Contains(t, out, "assert:supply_reconciles")
	assert.Contains(t, out, "no denom metadata configured")
}
