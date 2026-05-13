package checks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type TCPChecker struct {
	Options Options
}

func (c TCPChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	host, port, err := hostPort(monitor.Target, "")
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if !c.Options.AllowPrivateTargets {
		if err := validatePublicHost(ctx, host); err != nil {
			result.Error = err.Error()
			return result, err
		}
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := dialer.DialContext(checkCtx, "tcp", net.JoinHostPort(host, port))
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(start).Milliseconds()
	result.TCPConnectMS = result.TotalMS
	result.ResponseTimeMS = result.TotalMS
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			result.Error = fmt.Sprintf("tcp connect to %s:%s timed out after %s", host, port, timeout)
		default:
			result.Error = fmt.Sprintf("tcp connect to %s:%s failed: %v", host, port, err)
		}
		return result, err
	}
	_ = conn.Close()
	result.Success = true
	result.Status = models.StatusUp
	return result, nil
}
