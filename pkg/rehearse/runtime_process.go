package rehearse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// ProcessRuntime is the v1 Runtime: it boots the substitute validators as host child
// processes (no Docker / testcontainers). Each node runs in its own process group so
// teardown reliably reaps the whole tree. It mirrors smoke.sh's mechanics — patch config
// for fast blocks and distinct ports, wire persistent peers, start — and leaves readiness
// polling (height ≥ 1) to the engine via the returned RPC URL.
type ProcessRuntime struct {
	bin chainBinary
}

// NewProcessRuntime returns a process-based Runtime using the given chaind binary. The path
// must be the same operator-provisioned binary the engine sha256-verifies (Input.BinaryPath).
func NewProcessRuntime(binaryPath string) *ProcessRuntime {
	return &ProcessRuntime{bin: chainBinary{binaryPath}}
}

// Port bases; node i binds base + i*portStride so K localhost nodes never collide. gRPC and
// the API server are disabled at start (assertions query the RPC/ABCI endpoint), so only the
// RPC, P2P and pprof ports need per-node remapping.
const portStride = 100

const (
	basePortRPC   = 26657
	basePortP2P   = 26656
	basePortPProf = 6060
)

// teardownGrace is how long SIGTERM'd node groups get before SIGKILL.
const teardownGrace = 3 * time.Second

type nodeProc struct {
	cmd *exec.Cmd
	log *os.File
}

// processChain is a running ProcessRuntime chain. The engine owns the workdir (node homes);
// processChain owns only the running processes and their log files.
type processChain struct {
	procs  []nodeProc
	rpcURL string
}

func (p *processChain) RPCURL() string { return p.rpcURL }

// Boot patches each node's config (ports, peers, fast blocks), starts every node in its own
// process group, and returns immediately with a handle whose RPCURL is node 0. Readiness is
// the caller's to poll. Any error returns after tearing down whatever already started.
func (r *ProcessRuntime) Boot(ctx context.Context, homes []string) (Booted, error) {
	if len(homes) == 0 {
		return nil, errors.New("no node homes to boot")
	}

	nodeIDs := make([]string, len(homes))
	for i, home := range homes {
		id, err := r.nodeID(ctx, home)
		if err != nil {
			return nil, fmt.Errorf("node id for %s: %w", home, err)
		}
		nodeIDs[i] = id
	}
	peers := persistentPeers(nodeIDs)
	for i, home := range homes {
		if err := patchNodeConfig(home, i, peers); err != nil {
			return nil, fmt.Errorf("patch config for %s: %w", home, err)
		}
	}

	pc := &processChain{rpcURL: fmt.Sprintf("http://127.0.0.1:%d", basePortRPC)}
	for i, home := range homes {
		np, err := r.startNode(ctx, home)
		if err != nil {
			_ = pc.Teardown()
			return nil, fmt.Errorf("start node %d (%s): %w", i, home, err)
		}
		pc.procs = append(pc.procs, np)
	}
	return pc, nil
}

func (r *ProcessRuntime) nodeID(ctx context.Context, home string) (string, error) {
	out, err := r.bin.run(ctx, "comet", "show-node-id", "--home", home)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// startNode launches `chaind start` for one node home in its own process group, with gRPC and
// the API server disabled and a zero min-gas-price (no fee-paying txs are submitted). stdout
// and stderr stream to <home>/node.log for post-mortem diagnostics.
func (r *ProcessRuntime) startNode(ctx context.Context, home string) (nodeProc, error) {
	logFile, err := os.Create(filepath.Join(home, "node.log"))
	if err != nil {
		return nodeProc{}, err
	}
	//nolint:gosec // G204: trusted operator-provisioned binary + structured args, no shell.
	cmd := exec.CommandContext(ctx, r.bin.path,
		"start",
		"--home", home,
		"--minimum-gas-prices", "0stake",
		"--grpc.enable=false",
		"--api.enable=false",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd, syscall.SIGTERM) }
	cmd.WaitDelay = teardownGrace
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nodeProc{}, err
	}
	return nodeProc{cmd: cmd, log: logFile}, nil
}

// Teardown SIGTERMs every node's process group, waits a short grace, then SIGKILLs any
// survivor and reaps it. Safe to call more than once and on a partially-started chain.
func (p *processChain) Teardown() error {
	var errs []error
	for _, np := range p.procs {
		if np.cmd == nil || np.cmd.Process == nil {
			continue
		}
		if err := killGroup(np.cmd, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, err)
		}
	}
	time.Sleep(teardownGrace)
	for _, np := range p.procs {
		if np.cmd == nil || np.cmd.Process == nil {
			continue
		}
		_ = killGroup(np.cmd, syscall.SIGKILL) // best effort; the group may already be gone
		_ = np.cmd.Wait()                      // reap; a kill-signal exit is expected
		if np.log != nil {
			_ = np.log.Close()
		}
	}
	p.procs = nil
	return errors.Join(errs...)
}

// killGroup signals the whole process group led by cmd (Setpgid makes pgid == pid).
func killGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, sig)
}

func persistentPeers(nodeIDs []string) string {
	peers := make([]string, len(nodeIDs))
	for i, id := range nodeIDs {
		peers[i] = fmt.Sprintf("%s@127.0.0.1:%d", id, basePortP2P+i*portStride)
	}
	return strings.Join(peers, ",")
}

// patchNodeConfig remaps node idx's RPC/P2P/pprof ports, wires the persistent peers, and
// shortens the commit timeout for fast blocks. Each edit matches a cosmos default value or
// key, so an absent line is a harmless no-op.
func patchNodeConfig(home string, idx int, peers string) error {
	off := idx * portStride
	cfg := filepath.Join(home, "config", "config.toml")
	return patchTOML(cfg, [][2]string{
		{`(?m)^laddr = "tcp://127\.0\.0\.1:26657"$`, fmt.Sprintf(`laddr = "tcp://127.0.0.1:%d"`, basePortRPC+off)},
		{`(?m)^laddr = "tcp://0\.0\.0\.0:26656"$`, fmt.Sprintf(`laddr = "tcp://0.0.0.0:%d"`, basePortP2P+off)},
		{`(?m)^persistent_peers = .*$`, fmt.Sprintf(`persistent_peers = %q`, peers)},
		{`(?m)^allow_duplicate_ip = .*$`, `allow_duplicate_ip = true`},
		{`(?m)^addr_book_strict = .*$`, `addr_book_strict = false`},
		{`(?m)^timeout_commit = .*$`, `timeout_commit = "1s"`},
		{`(?m)^pprof_laddr = .*$`, fmt.Sprintf(`pprof_laddr = "localhost:%d"`, basePortPProf+off)},
	})
}

// patchTOML applies each {pattern, replacement} to the file in order. Replacements are
// literal (no $-expansion), so peer lists and addresses pass through untouched.
func patchTOML(path string, edits [][2]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)
	for _, e := range edits {
		re, err := regexp.Compile(e[0])
		if err != nil {
			return err
		}
		s = re.ReplaceAllLiteralString(s, e[1])
	}
	return os.WriteFile(path, []byte(s), 0o600)
}
