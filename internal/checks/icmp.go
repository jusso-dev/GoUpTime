// ICMP ping monitor. Uses pro-bing's unprivileged UDP-based ICMP so the
// container does not need CAP_NET_RAW. The host (or k8s pod) must allow
// the calling UID via the net.ipv4.ping_group_range sysctl — documented
// in the project README.
//
// Risk: cloud platforms that block outbound ICMP entirely (Fargate) will
// always report this monitor as down. Operators should fall back to a
// TCP-echo check (port 7) for those targets.

package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"github.com/jusso-dev/uptime/internal/models"
)

type ICMPChecker struct {
	Options Options
}

func (c ICMPChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	host := strings.TrimSpace(monitor.Target)
	if host == "" {
		err := fmt.Errorf("target host is required")
		result.Error = err.Error()
		return result, err
	}
	if strings.ContainsAny(host, "/?#") || strings.Contains(host, "://") {
		err := fmt.Errorf("icmp target must be a hostname or ip, not a url: %q", host)
		result.Error = err.Error()
		return result, err
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pinger, err := probing.NewPinger(host)
	if err != nil {
		result.Error = fmt.Sprintf("resolve ping target %q: %v", host, err)
		return result, err
	}
	// Unprivileged mode: kernel-mediated UDP rather than raw sockets.
	pinger.SetPrivileged(false)
	pinger.Count = 3
	pinger.Timeout = timeout
	// pro-bing's Run blocks on its own; bind it to our context via a
	// goroutine so cancellation hard-stops the ping.
	done := make(chan error, 1)
	go func() {
		done <- pinger.RunWithContext(checkCtx)
	}()
	select {
	case <-checkCtx.Done():
		pinger.Stop()
		result.Error = "icmp probe timed out"
		return result, checkCtx.Err()
	case err = <-done:
	}

	if err != nil {
		result.Error = fmt.Sprintf("icmp probe failed: %v", err)
		return result, err
	}
	stats := pinger.Statistics()
	result.CheckedAt = time.Now().UTC()
	result.ResponseTimeMS = stats.AvgRtt.Milliseconds()
	result.TotalMS = stats.AvgRtt.Milliseconds()

	if stats.PacketsRecv == 0 {
		result.Error = fmt.Sprintf("no icmp replies (sent %d)", stats.PacketsSent)
		return result, nil
	}
	if stats.PacketLoss > 50 {
		result.Status = models.StatusDegraded
		result.Success = false
		result.Error = fmt.Sprintf("icmp packet loss %.1f%% (sent %d, recv %d)",
			stats.PacketLoss, stats.PacketsSent, stats.PacketsRecv)
		return result, nil
	}
	result.Success = true
	result.Status = models.StatusUp
	return result, nil
}
