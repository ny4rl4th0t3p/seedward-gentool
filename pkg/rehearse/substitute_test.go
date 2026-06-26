package rehearse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPartitionSelfDelegation_SumPreservedAndPositive(t *testing.T) {
	cases := []struct {
		total int64
		k     int
	}{
		{1000000, 2}, {7, 3}, {5, 5}, {100, 1}, {1000001, 2},
	}
	for _, c := range cases {
		parts := partitionSelfDelegation(c.total, c.k)
		require.Len(t, parts, c.k)
		var sum int64
		for _, p := range parts {
			assert.Positive(t, p, "each part must be positive (total=%d k=%d)", c.total, c.k)
			sum += p
		}
		assert.Equal(t, c.total, sum, "partition must preserve the total (total=%d k=%d)", c.total, c.k)
	}
}

func TestClampValidators(t *testing.T) {
	assert.Equal(t, 2, clampValidators(2, 1000000)) // within range
	assert.Equal(t, 1, clampValidators(0, 1000000)) // floor at 1
	assert.Equal(t, 3, clampValidators(5, 3))       // cannot exceed the total
	assert.Equal(t, 2, clampValidators(2, 0))       // degenerate total → left for the boot build
}

func TestRemapClaimDelegations(t *testing.T) {
	remap := map[string]string{"val-a": "rehearse-val-0", "val-b": "rehearse-val-1"}
	in := "cosmos1aaa,1000,val-a\ncosmos1bbb,500\ncosmos1ccc,2000,val-b\n"

	out := string(remapClaimDelegations([]byte(in), remap))

	// delegate_to remapped; address/amount untouched; the non-delegating row passes through.
	assert.Contains(t, out, "cosmos1aaa,1000,rehearse-val-0")
	assert.Contains(t, out, "cosmos1ccc,2000,rehearse-val-1")
	assert.Contains(t, out, "cosmos1bbb,500")
	assert.NotContains(t, out, "val-a")
	assert.NotContains(t, out, "val-b")
}

func TestRemapClaimDelegations_UnknownMonikerUntouched(t *testing.T) {
	// A delegate_to not in the map is left as-is (it can't occur once the real build passed).
	out := string(remapClaimDelegations([]byte("cosmos1aaa,1000,mystery\n"), map[string]string{"val-a": "rehearse-val-0"}))
	assert.Contains(t, out, "mystery")
}
