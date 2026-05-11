package checks

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"syscall"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type HTTPChecker struct {
	Options Options
}

func (c HTTPChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	u, err := ValidateURL(monitor.Target, c.Options.AllowPrivateTargets)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	method := strings.ToUpper(monitor.Method)
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		err := fmt.Errorf("unsupported http method %q", method)
		result.Error = err.Error()
		return result, err
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	timings := &traceTimings{start: time.Now()}
	req, err := http.NewRequestWithContext(httptraceContext(checkCtx, timings), method, u.String(), nil)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	req.Header.Set("User-Agent", c.Options.UserAgent)

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           safeDialer(c.Options.AllowPrivateTargets).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(timings.start).Milliseconds()
	result.ResponseTimeMS = result.TotalMS
	result.DNSMS = timings.dnsMS
	result.TCPConnectMS = timings.tcpConnectMS
	result.TLSHandshakeMS = timings.tlsHandshakeMS
	result.TimeToFirstByteMS = timings.timeToFirstByteMS
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	if method != http.MethodHead {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxSnippetBytes))
		result.ResponseSnippet = string(body)
	}

	expected := monitor.ExpectedStatus
	if expected == 0 {
		expected = http.StatusOK
	}
	success := resp.StatusCode == expected
	if monitor.Type == models.MonitorKeyword && monitor.ExpectedKeyword != "" && !strings.Contains(result.ResponseSnippet, monitor.ExpectedKeyword) {
		success = false
		result.Error = "expected keyword not found"
	}
	if !success && result.Error == "" {
		result.Error = fmt.Sprintf("expected status %d, got %d", expected, resp.StatusCode)
	}
	result.Success = success
	if success {
		result.Status = models.StatusUp
	}
	return result, nil
}

func httptraceContext(ctx context.Context, timings *traceTimings) context.Context {
	return httptrace.WithClientTrace(ctx, timings.clientTrace())
}

func safeDialer(allowPrivate bool) *net.Dialer {
	return &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return err
			}
			for _, ip := range ips {
				if !isPublicIP(ip) {
					return ErrBlockedTarget
				}
			}
			return nil
		},
	}
}
