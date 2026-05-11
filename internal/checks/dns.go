package checks

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type DNSChecker struct {
	Options Options
}

func (c DNSChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	host := strings.TrimSpace(monitor.Target)
	if host == "" {
		err := fmt.Errorf("target host is required")
		result.Error = err.Error()
		return result, err
	}
	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	ips, err := net.DefaultResolver.LookupIPAddr(checkCtx, host)
	result.CheckedAt = time.Now().UTC()
	result.DNSMS = time.Since(start).Milliseconds()
	result.TotalMS = result.DNSMS
	result.ResponseTimeMS = result.TotalMS
	if err != nil || len(ips) == 0 {
		if err == nil {
			err = fmt.Errorf("host did not resolve")
		}
		result.Error = err.Error()
		return result, err
	}
	result.Success = true
	result.Status = models.StatusUp
	return result, nil
}
