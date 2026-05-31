// Domain expiry monitor. Queries RDAP (RFC 9082/9083) for the registered
// domain's expiration date and reports degraded when it's within
// Options.TLSExpiryWarnDays (reusing the same knob — operators tend to
// want a single "renew threshold" rather than per-check-type tuning) and
// down when it's expired.

package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openrdap/rdap"

	"github.com/jusso-dev/uptime/internal/models"
)

type DomainChecker struct {
	Options Options
}

func (c DomainChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	domain := strings.ToLower(strings.TrimSpace(monitor.Target))
	if domain == "" {
		err := fmt.Errorf("target domain is required")
		result.Error = err.Error()
		return result, err
	}
	// RDAP needs the registered domain, not a URL. Strip schemes/paths to
	// be forgiving about operator input.
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	if i := strings.IndexAny(domain, "/?#"); i > 0 {
		domain = domain[:i]
	}
	if strings.Contains(domain, ":") {
		domain = strings.Split(domain, ":")[0]
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &rdap.Client{}
	start := time.Now()
	domainObj, err := client.QueryDomain(domain)
	_ = checkCtx
	result.CheckedAt = time.Now().UTC()
	result.ResponseTimeMS = time.Since(start).Milliseconds()
	result.TotalMS = result.ResponseTimeMS
	if err != nil {
		result.Error = fmt.Sprintf("rdap lookup for %q failed: %v", domain, err)
		return result, err
	}

	var expiresAt time.Time
	for _, ev := range domainObj.Events {
		if strings.EqualFold(ev.Action, "expiration") {
			t, parseErr := time.Parse(time.RFC3339, ev.Date)
			if parseErr == nil {
				expiresAt = t
				break
			}
		}
	}
	if expiresAt.IsZero() {
		result.Error = fmt.Sprintf("rdap response for %q did not include an expiration date", domain)
		return result, nil
	}

	result.DomainExpiresAt = &expiresAt
	remaining := time.Until(expiresAt)
	warnDays := c.Options.TLSExpiryWarnDays
	if warnDays <= 0 {
		warnDays = 14
	}
	switch {
	case remaining <= 0:
		result.Error = fmt.Sprintf("domain %q expired on %s", domain, expiresAt.Format("2006-01-02"))
		return result, nil
	case remaining < time.Duration(warnDays)*24*time.Hour:
		result.Success = true
		result.Status = models.StatusDegraded
		result.Error = fmt.Sprintf("domain %q expires in %s", domain, remaining.Round(time.Hour))
		return result, nil
	default:
		result.Success = true
		result.Status = models.StatusUp
		return result, nil
	}
}
