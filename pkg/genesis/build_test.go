package genesis

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The happy path of Build (full pipeline against a real baseline genesis) is
// exercised end-to-end by the Docker smoke test, which now routes cmd/gentool
// through Build. These cover the cheap pre-flight validation that needs no fixture.

func TestBuild_RequiresAddressPrefix(t *testing.T) {
	_, err := Build(context.Background(), nil, ChainConfig{BondDenom: "uatom"}, Repositories{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AddressPrefix")
}

func TestBuild_RequiresBondDenom(t *testing.T) {
	_, err := Build(context.Background(), nil, ChainConfig{AddressPrefix: testHRP}, Repositories{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BondDenom")
}
