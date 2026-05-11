package checks

import (
	"context"
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

	start := time.Now()
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(checkCtx, "tcp", net.JoinHostPort(host, port))
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(start).Milliseconds()
	result.TCPConnectMS = result.TotalMS
	result.ResponseTimeMS = result.TotalMS
	if err != nil {
		result.Error = fmt.Sprintf("tcp connect failed: %v", err)
		return result, err
	}
	_ = conn.Close()
	result.Success = true
	result.Status = models.StatusUp
	return result, nil
}
