package rehearse

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTempBinary(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chaind")
	require.NoError(t, os.WriteFile(path, content, 0o755))
	return path
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestVerifyBinary_Match(t *testing.T) {
	content := []byte("fake-chaind-bytes")
	path := writeTempBinary(t, content)
	require.NoError(t, verifyBinary(path, sha256Hex(content)))
}

func TestVerifyBinary_MatchCaseInsensitive(t *testing.T) {
	content := []byte("fake-chaind-bytes")
	path := writeTempBinary(t, content)
	require.NoError(t, verifyBinary(path, strings.ToUpper(sha256Hex(content))))
}

func TestVerifyBinary_Mismatch(t *testing.T) {
	path := writeTempBinary(t, []byte("actual"))
	err := verifyBinary(path, sha256Hex([]byte("different")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mismatch")
}

func TestVerifyBinary_EmptyExpectedSkipsDigest(t *testing.T) {
	// Empty expected hash still requires the file to exist, but skips the digest check.
	path := writeTempBinary(t, []byte("whatever"))
	require.NoError(t, verifyBinary(path, ""))
}

func TestVerifyBinary_Missing(t *testing.T) {
	require.Error(t, verifyBinary(filepath.Join(t.TempDir(), "nope"), "abc"))
}
