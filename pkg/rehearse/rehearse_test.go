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
	require.Len(t, res.Steps, 1)
	assert.Equal(t, StepFail, res.Steps[0].Status)
}
