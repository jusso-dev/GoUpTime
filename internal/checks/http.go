package checks

import (
	"context"
	"errors"
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

// maxResponseBytes caps how many bytes we read from the response body even
// for small responses. Hard-limits the worst case so a malicious server
// can't stream gigabytes back and starve the worker.
const maxResponseBytes = 1 << 20 // 1 MiB

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

	method := strings.ToUpper(strings.TrimSpace(monitor.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		err := fmt.Errorf("unsupported http method %q (only GET and HEAD)", method)
		result.Error = err.Error()
		return result, err
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	timings := &traceTimings{start: time.Now()}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(checkCtx, timings.clientTrace()), method, u.String(), nil)
	if err != nil {
		result.Error = fmt.Sprintf("build http request: %v", err)
		return result, err
	}
	req.Header.Set("User-Agent", c.Options.UserAgent)
	req.Header.Set("Accept", "*/*")

	// Per-check sub-timeouts. TLSHandshakeTimeout shorter than the overall
	// timeout keeps a slow handshake from eating the whole budget.
	tlsTimeout := timeout / 2
	if tlsTimeout < time.Second {
		tlsTimeout = time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           safeDialer(c.Options.AllowPrivateTargets, timeout).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		// One-shot per check: no keep-alive pooling.
		DisableKeepAlives:  true,
		MaxIdleConns:       0,
		IdleConnTimeout:    time.Second,
		DisableCompression: false,
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// Redirects intentionally disabled: every redirect changes the
		// effective target, defeating the SSRF guard and obscuring what
		// the operator actually configured.
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
		result.Error = classifyHTTPError(err, checkCtx, timeout)
		return result, err
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	if method != http.MethodHead {
		// LimitReader caps how much we *try* to read; MaxBytes-style is
		// not required because the body is closed immediately after.
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			result.Error = fmt.Sprintf("read response body: %v", readErr)
			return result, readErr
		}
		// Drain a little more so the connection can be reused if keep-alives
		// were ever turned on; cheap insurance.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if len(body) > maxSnippetBytes {
			result.ResponseSnippet = string(body[:maxSnippetBytes])
		} else {
			result.ResponseSnippet = string(body)
		}
	}

	expected := monitor.ExpectedStatus
	if expected == 0 {
		expected = http.StatusOK
	}
	success := resp.StatusCode == expected
	if monitor.Type == models.MonitorKeyword && monitor.ExpectedKeyword != "" && !strings.Contains(result.ResponseSnippet, monitor.ExpectedKeyword) {
		success = false
		result.Error = fmt.Sprintf("expected keyword %q not found in response", monitor.ExpectedKeyword)
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

// classifyHTTPError produces a stable, operator-friendly error message
// distinguishing context cancellation, deadline exceeded, DNS failure, and
// TLS/connection errors from a generic transport error.
func classifyHTTPError(err error, ctx context.Context, timeout time.Duration) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("request timed out after %s", timeout)
	case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
		return "request cancelled"
	case errors.Is(err, ErrBlockedTarget):
		return ErrBlockedTarget.Error()
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Sprintf("dns lookup failed: %s", dnsErr.Err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("network timeout after %s", timeout)
	}
	return err.Error()
}

// safeDialer constructs a net.Dialer with a Control hook that re-validates
// the destination address right before connect. Re-checking here (not just
// in ValidateURL) defends against DNS rebinding: a hostname that resolved to
// a public IP a second ago could now resolve to 169.254.169.254.
func safeDialer(allowPrivate bool, timeout time.Duration) *net.Dialer {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &net.Dialer{
		Timeout:   timeout,
		KeepAlive: -1, // we use DisableKeepAlives on the transport
		Control: func(_, address string, _ syscall.RawConn) error {
			if allowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("split dial address %q: %w", address, err)
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// At this point the resolver has produced an IP; if we
				// see a non-IP, refuse rather than re-resolve.
				return fmt.Errorf("expected IP address at dial time, got %q", host)
			}
			if !isPublicIP(ip) {
				return ErrBlockedTarget
			}
			return nil
		},
	}
}
