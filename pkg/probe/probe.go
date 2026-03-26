package probe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

type Result struct {
	Detected bool
	Reason   string
}

type Hook interface {
	ObserveLatency(target string, latency time.Duration, err error)
}

func Run(ctx context.Context) Result {
	return RunWithHook(ctx, nil)
}

func RunWithHook(ctx context.Context, hook Hook) Result {
	blocked := "https://rutracker.org"
	good := "https://www.google.com/generate_204"

	goodLatency, gErr := probeHTTP(ctx, good)
	if hook != nil {
		hook.ObserveLatency(good, goodLatency, gErr)
	}
	blockedLatency, bErr := probeHTTP(ctx, blocked)
	if hook != nil {
		hook.ObserveLatency(blocked, blockedLatency, bErr)
	}

	if isRST(gErr) || isRST(bErr) {
		return Result{Detected: true, Reason: "tcp reset"}
	}
	if bErr != nil && gErr == nil {
		return Result{Detected: true, Reason: "selective blocking"}
	}
	if blockedLatency > 0 && goodLatency > 0 && blockedLatency > goodLatency*4 {
		return Result{Detected: true, Reason: "throttle/dpi signature"}
	}
	return Result{}
}

func probeHTTP(ctx context.Context, target string) (time.Duration, error) {
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 500 {
		return 0, errors.New("upstream 5xx")
	}
	return time.Since(start), nil
}

func isRST(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) {
		if op.Err != nil && (op.Err.Error() == "connection reset by peer" || op.Err.Error() == "wsarecv: An existing connection was forcibly closed by the remote host.") {
			return true
		}
	}
	return false
}
