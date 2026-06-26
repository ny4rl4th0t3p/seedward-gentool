package rehearse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistentPeers(t *testing.T) {
	got := persistentPeers([]string{"aaa", "bbb"})
	assert.Equal(t, "aaa@127.0.0.1:26656,bbb@127.0.0.1:26756", got)
}

// patchNodeConfig is the riskiest part of the runtime (regex surgery on config.toml); it is
// pure and binary-free, so it gets a real unit test (the boot itself is integration).
func TestPatchNodeConfig_RemapsPortsAndPeers(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o700))
	const sample = `laddr = "tcp://127.0.0.1:26657"
laddr = "tcp://0.0.0.0:26656"
persistent_peers = ""
addr_book_strict = true
allow_duplicate_ip = false
timeout_commit = "5s"
pprof_laddr = "localhost:6060"
`
	cfg := filepath.Join(home, "config", "config.toml")
	require.NoError(t, os.WriteFile(cfg, []byte(sample), 0o600))

	require.NoError(t, patchNodeConfig(home, 1, "node0@127.0.0.1:26656"))

	out, err := os.ReadFile(cfg)
	require.NoError(t, err)
	got := string(out)
	assert.Contains(t, got, `laddr = "tcp://127.0.0.1:26757"`)
	assert.Contains(t, got, `laddr = "tcp://0.0.0.0:26756"`)
	assert.Contains(t, got, `persistent_peers = "node0@127.0.0.1:26656"`)
	assert.Contains(t, got, `addr_book_strict = false`)
	assert.Contains(t, got, `allow_duplicate_ip = true`)
	assert.Contains(t, got, `timeout_commit = "1s"`)
	assert.Contains(t, got, `pprof_laddr = "localhost:6160"`)
}
