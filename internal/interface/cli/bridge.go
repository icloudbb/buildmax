package cli

import (
	"context"
	"net"
	"net/http"
	"os"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
)

// workerBridge is how a command reaches the server when it runs inside a worker
// run rather than a local session. The run token was scrubbed from the
// environment; the bridge socket carries it. The command speaks the worker API
// through the socket and never sees the token.
type workerBridge struct {
	cfg       workerclient.WorkerAPIClientConfig
	taskRunID string
}

// inWorkerRun returns the run bridge when this process is a subprocess of a
// worker run — BUILDMAX_BRIDGE_SOCK and BUILDMAX_TASK_RUN_ID are set — and nil
// in a local session, which is what selects a command's context. The socket
// path, not a credential, is the whole handle: the bridge attaches the token.
func inWorkerRun() *workerBridge {
	sock := os.Getenv(config.EnvKeyBuildmaxBridgeSock)
	runID := os.Getenv(config.EnvKeyBuildmaxTaskRunID)
	if sock == "" || runID == "" {
		return nil
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		},
	}
	return &workerBridge{
		// The host is a placeholder the dialer ignores; the worker client appends
		// the /api/worker/task-runs/{id}/... paths itself. Token is empty because
		// the bridge injects the run token before forwarding.
		cfg:       workerclient.WorkerAPIClientConfig{BaseURL: "http://bridge", Client: client},
		taskRunID: runID,
	}
}
