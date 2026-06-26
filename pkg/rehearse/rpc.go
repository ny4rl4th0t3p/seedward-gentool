package rehearse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// rpcClient queries a booted node's CometBFT RPC endpoint. It targets the stable
// result.sync_info schema, so it works across cosmos-sdk / CometBFT versions.
type rpcClient struct {
	base string
	hc   *http.Client
}

// pollInterval is how often waitForHeight re-queries the node's RPC.
const pollInterval = 500 * time.Millisecond

func newRPCClient(base string) rpcClient {
	return rpcClient{base: base, hc: &http.Client{Timeout: 5 * time.Second}}
}

// height returns the latest committed block height (0 before the first commit). A non-nil
// error means the endpoint is not reachable yet.
func (c rpcClient) height(ctx context.Context) (int64, error) {
	var out struct {
		Result struct {
			SyncInfo struct {
				LatestBlockHeight string `json:"latest_block_height"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := c.getJSON(ctx, "/status", &out); err != nil {
		return 0, err
	}
	if out.Result.SyncInfo.LatestBlockHeight == "" {
		return 0, nil
	}
	return strconv.ParseInt(out.Result.SyncInfo.LatestBlockHeight, 10, 64)
}

func (c rpcClient) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rpc GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// waitForHeight polls the node's RPC until the chain reaches target height or timeout. A
// not-yet-reachable endpoint is tolerated (kept polling) until the deadline.
func waitForHeight(ctx context.Context, rpcURL string, target int64, timeout time.Duration) error {
	cli := newRPCClient(rpcURL)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		h, err := cli.height(ctx)
		switch {
		case err != nil:
			lastErr = err
		case h >= target:
			return nil
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("height %d not reached within %s: %w", target, timeout, lastErr)
			}
			return fmt.Errorf("height %d not reached within %s", target, timeout)
		case <-ticker.C:
		}
	}
}
