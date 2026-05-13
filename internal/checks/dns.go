package checks

import (
	"context"
	"errors"
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
	// Reject anything that looks like a URL or host:port — DNS checks
	// take a hostname only and the implicit fallback would silently
	// resolve something the operator did not intend.
	if strings.ContainsAny(host, "/?#") || strings.Contains(host, "://") {
		err := fmt.Errorf("dns target must be a hostname, not a url: %q", host)
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
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			result.Error = fmt.Sprintf("dns lookup for %q failed: %s", host, dnsErr.Err)
		} else {
			result.Error = fmt.Sprintf("dns lookup for %q failed: %v", host, err)
		}
		return result, err
	}
	if len(ips) == 0 {
		err := fmt.Errorf("host %q did not resolve to any addresses", host)
		result.Error = err.Error()
		return result, err
	}
	result.Success = true
	result.Status = models.StatusUp
	return result, nil
}
