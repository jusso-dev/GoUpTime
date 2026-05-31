package checks

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type DomainChecker struct {
	Options Options
}

func (c DomainChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	domain := strings.TrimSpace(strings.TrimPrefix(monitor.Target, "https://"))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.Trim(domain, "/")
	if domain == "" || strings.ContainsAny(domain, "/?#:") {
		err := fmt.Errorf("domain target must be a bare domain name")
		result.Error = err.Error()
		return result, err
	}
	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, "https://rdap.org/domain/"+domain, nil)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	req.Header.Set("User-Agent", c.Options.UserAgent)
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(start).Milliseconds()
	result.ResponseTimeMS = result.TotalMS
	if err != nil {
		result.Error = fmt.Sprintf("rdap lookup failed: %v", err)
		return result, err
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return c.domainFromWHOIS(ctx, monitor, domain, result, start)
	}
	var rdap struct {
		Events []struct {
			EventAction string `json:"eventAction"`
			EventDate   string `json:"eventDate"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rdap); err != nil {
		result.Error = fmt.Sprintf("decode rdap response: %v", err)
		return result, err
	}
	var expiry time.Time
	for _, event := range rdap.Events {
		action := strings.ToLower(event.EventAction)
		if action != "expiration" && action != "expiry" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, event.EventDate)
		if err == nil {
			expiry = parsed
			break
		}
	}
	if expiry.IsZero() {
		return c.domainFromWHOIS(ctx, monitor, domain, result, start)
	}
	return classifyDomainExpiry(result, monitor, expiry), nil
}

func (c DomainChecker) domainFromWHOIS(ctx context.Context, monitor models.Monitor, domain string, result models.CheckResult, start time.Time) (models.CheckResult, error) {
	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	server, err := whoisServer(ctx, domain, timeout)
	if err != nil {
		result.Error = fmt.Sprintf("whois server lookup failed: %v", err)
		return result, err
	}
	expiry, err := whoisExpiry(ctx, server, domain, timeout)
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(start).Milliseconds()
	result.ResponseTimeMS = result.TotalMS
	if err != nil {
		result.Error = fmt.Sprintf("whois lookup failed: %v", err)
		return result, err
	}
	if expiry.IsZero() {
		result.Error = "whois response did not include domain expiration"
		return result, nil
	}
	return classifyDomainExpiry(result, monitor, expiry), nil
}

func classifyDomainExpiry(result models.CheckResult, monitor models.Monitor, expiry time.Time) models.CheckResult {
	warnDays := configNumber(monitor.Config, "expiryWarnDays", 30)
	daysRemaining := int(time.Until(expiry).Hours() / 24)
	result.Metadata = map[string]any{
		"expiresAt":     expiry.UTC().Format(time.RFC3339),
		"daysRemaining": daysRemaining,
	}
	result.Success = true
	result.Status = models.StatusUp
	if daysRemaining < 0 {
		result.Success = false
		result.Status = models.StatusDown
		result.Error = fmt.Sprintf("domain expired at %s", expiry.UTC().Format(time.RFC3339))
	} else if daysRemaining <= warnDays {
		result.Status = models.StatusDegraded
		result.Error = fmt.Sprintf("domain expires soon at %s", expiry.UTC().Format(time.RFC3339))
	}
	return result
}

func whoisServer(ctx context.Context, domain string, timeout time.Duration) (string, error) {
	tld := domain
	if dot := strings.LastIndex(domain, "."); dot >= 0 && dot < len(domain)-1 {
		tld = domain[dot+1:]
	}
	lines, err := queryWHOIS(ctx, "whois.iana.org:43", tld, timeout)
	if err != nil {
		return "", err
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "whois:") {
			server := strings.TrimSpace(strings.TrimPrefix(line, "whois:"))
			if server != "" {
				return server + ":43", nil
			}
		}
	}
	return "", fmt.Errorf("no whois server found for .%s", tld)
}

func whoisExpiry(ctx context.Context, server, domain string, timeout time.Duration) (time.Time, error) {
	lines, err := queryWHOIS(ctx, server, domain, timeout)
	if err != nil {
		return time.Time{}, err
	}
	keys := []string{"registry expiry date:", "registrar registration expiration date:", "expiration date:", "expiry date:"}
	for _, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		for _, key := range keys {
			if strings.HasPrefix(lower, key) {
				value := strings.TrimSpace(line[len(key):])
				if parsed, ok := parseDomainTime(value); ok {
					return parsed, nil
				}
			}
		}
	}
	return time.Time{}, nil
}

func queryWHOIS(ctx context.Context, address, query string, timeout time.Duration) ([]string, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintf(conn, "%s\r\n", query); err != nil {
		return nil, err
	}
	lines := []string{}
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func parseDomainTime(value string) (time.Time, bool) {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"02-Jan-2006",
	}
	for _, format := range formats {
		if parsed, err := time.Parse(format, strings.TrimSpace(value)); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func configNumber(config map[string]any, key string, fallback int) int {
	if config == nil {
		return fallback
	}
	switch value := config[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return fallback
}
