package rehearse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis"
	"github.com/ny4rl4th0t3p/seedward-gentool/pkg/genesis/encoding"
)

func TestWriteGentxs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gentx")
	require.NoError(t, writeGentxs(dir, [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`)}))
	for i, want := range []string{`{"a":1}`, `{"b":2}`} {
		got, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("gentx-%d.json", i)))
		require.NoError(t, err)
		assert.Equal(t, want, string(got))
	}
}

func TestComputeModuleAddresses_IncludesStandardAndExtra(t *testing.T) {
	base := computeModuleAddresses("cosmos", nil)
	withExtra := computeModuleAddresses("cosmos", []genesis.ExtraModule{{Name: "meta"}})
	assert.NotEmpty(t, base)
	assert.GreaterOrEqual(t, len(base), len(encoding.StandardModuleNames))
	assert.Greater(t, len(withExtra), len(base), "extra module should contribute an address")
}

// accounts is required: an input without it fails at the build step (FAIL) before any
// materialize/boot, so a nil Runtime and no real chain binary are fine here.
func TestRun_MissingAccounts_IsFailBuild(t *testing.T) {
	path := writeTempBinary(t, []byte("bin")) // empty sha → verify skips the digest
	e := New(nil)

	res, err := e.Run(context.Background(), Input{
		BinaryPath:  path,
		Config:      genesis.ChainConfig{AddressPrefix: "cosmos", BondDenom: "uatom", ChainID: "test-1"},
		Allocations: map[AllocationType][]byte{}, // no accounts
		Gentxs:      [][]byte{[]byte(`{}`)},
	})

	require.NoError(t, err)
	assert.Equal(t, OutcomeFail, res.Outcome)
	assert.Equal(t, "build", res.FailedStep)
	assert.ErrorIs(t, res.Err, ErrInvalidInput)
}
