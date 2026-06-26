package rehearse

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// verifyBinary checks the chaind binary at path against expectedSHA256 (hex). It always
// confirms the file is readable; when expectedSHA256 is non-empty it additionally verifies
// the digest. An empty expected hash skips the digest check — standalone callers may not
// have one, while coordd-driven runs always supply it (bridge §2). A returned error is
// terminal and surfaces as an ERROR outcome (infra fault, not a verdict on the genesis).
func verifyBinary(path, expectedSHA256 string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open chain binary %q: %w", path, err)
	}
	defer f.Close()

	if expectedSHA256 == "" {
		return nil
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing chain binary %q: %w", path, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, expectedSHA256) {
		return fmt.Errorf("chain binary sha256 mismatch: got %s, want %s", got, expectedSHA256)
	}
	return nil
}
