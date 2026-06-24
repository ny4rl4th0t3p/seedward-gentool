package rehearse

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// chainBinary wraps a chaind binary path for invoking its subcommands.
type chainBinary struct{ path string }

// run executes the binary with args and returns its combined output. On failure the error
// includes the command and captured output (chaind writes diagnostics to stderr).
func (c chainBinary) run(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, c.path, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w\n%s", filepath.Base(c.path), strings.Join(args, " "), err, out)
	}
	return out, nil
}
