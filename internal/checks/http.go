package checks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"reflect"
	"regexp"
	"strconv"
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
	if !methodAllowed(monitor.Type, method) {
		err := fmt.Errorf("unsupported http method %q for %s monitor", method, monitor.Type)
		result.Error = err.Error()
		return result, err
	}

	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	timings := &traceTimings{start: time.Now()}
	bodyReader, err := requestBody(monitor)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(checkCtx, timings.clientTrace()), method, u.String(), bodyReader)
	if err != nil {
		result.Error = fmt.Sprintf("build http request: %v", err)
		return result, err
	}
	req.Header.Set("User-Agent", c.Options.UserAgent)
	req.Header.Set("Accept", "*/*")
	applyRequestConfig(req, monitor)

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
	if monitor.Type == models.MonitorAPI && success {
		if err := validateAPIAssertions(result.ResponseSnippet, monitor.Config); err != nil {
			success = false
			result.Error = err.Error()
		}
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

func methodAllowed(monitorType models.MonitorType, method string) bool {
	if monitorType == models.MonitorAPI {
		switch method {
		case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
			return true
		default:
			return false
		}
	}
	return method == http.MethodGet || method == http.MethodHead
}

func requestBody(monitor models.Monitor) (io.Reader, error) {
	if monitor.Type != models.MonitorAPI || monitor.Config == nil {
		return nil, nil
	}
	value, ok := monitor.Config["body"]
	if !ok || value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		return strings.NewReader(typed), nil
	case []byte:
		return bytes.NewReader(typed), nil
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		return bytes.NewReader(b), nil
	}
}

func applyRequestConfig(req *http.Request, monitor models.Monitor) {
	if monitor.Type != models.MonitorAPI || monitor.Config == nil {
		return
	}
	if headers, ok := stringMap(monitor.Config["headers"]); ok {
		for key, value := range headers {
			if strings.TrimSpace(key) != "" {
				req.Header.Set(key, value)
			}
		}
	}
	if req.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token, _ := monitor.Config["bearerToken"].(string); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	username, _ := monitor.Config["basicAuthUsername"].(string)
	password, _ := monitor.Config["basicAuthPassword"].(string)
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}
}

func stringMap(value any) (map[string]string, bool) {
	switch typed := value.(type) {
	case map[string]string:
		return typed, true
	case map[string]any:
		out := map[string]string{}
		for k, v := range typed {
			out[k] = fmt.Sprint(v)
		}
		return out, true
	default:
		return nil, false
	}
}

type apiAssertion struct {
	Path     string `json:"path"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

func validateAPIAssertions(snippet string, config map[string]any) error {
	if config == nil || config["assertions"] == nil {
		return nil
	}
	assertions, err := parseAssertions(config["assertions"])
	if err != nil {
		return err
	}
	if len(assertions) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal([]byte(snippet), &payload); err != nil {
		return fmt.Errorf("parse json response for assertions: %w", err)
	}
	for _, assertion := range assertions {
		got, exists := jsonPath(payload, assertion.Path)
		if err := evaluateAssertion(assertion, got, exists); err != nil {
			return err
		}
	}
	return nil
}

func parseAssertions(value any) ([]apiAssertion, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal assertions: %w", err)
	}
	var assertions []apiAssertion
	if err := json.Unmarshal(b, &assertions); err != nil {
		return nil, fmt.Errorf("parse assertions: %w", err)
	}
	return assertions, nil
}

func evaluateAssertion(assertion apiAssertion, got any, exists bool) error {
	op := assertion.Operator
	if op == "" {
		op = "equals"
	}
	switch op {
	case "exists":
		if !exists {
			return fmt.Errorf("assertion failed: %s does not exist", assertion.Path)
		}
	case "equals":
		if !exists || !valuesEqual(got, assertion.Value) {
			return fmt.Errorf("assertion failed: %s expected %v, got %v", assertion.Path, assertion.Value, got)
		}
	case "notEquals":
		if exists && valuesEqual(got, assertion.Value) {
			return fmt.Errorf("assertion failed: %s should not equal %v", assertion.Path, assertion.Value)
		}
	case "contains":
		if !strings.Contains(fmt.Sprint(got), fmt.Sprint(assertion.Value)) {
			return fmt.Errorf("assertion failed: %s does not contain %v", assertion.Path, assertion.Value)
		}
	case "greaterThan", "lessThan":
		gotNumber, ok := numberValue(got)
		if !ok {
			return fmt.Errorf("assertion failed: %s is not numeric", assertion.Path)
		}
		wantNumber, ok := numberValue(assertion.Value)
		if !ok {
			return fmt.Errorf("assertion failed: expected value for %s is not numeric", assertion.Path)
		}
		if op == "greaterThan" && gotNumber <= wantNumber {
			return fmt.Errorf("assertion failed: %s expected > %v, got %v", assertion.Path, wantNumber, gotNumber)
		}
		if op == "lessThan" && gotNumber >= wantNumber {
			return fmt.Errorf("assertion failed: %s expected < %v, got %v", assertion.Path, wantNumber, gotNumber)
		}
	case "matchesRegex":
		matched, err := regexp.MatchString(fmt.Sprint(assertion.Value), fmt.Sprint(got))
		if err != nil {
			return fmt.Errorf("assertion failed: invalid regex for %s: %w", assertion.Path, err)
		}
		if !matched {
			return fmt.Errorf("assertion failed: %s did not match %v", assertion.Path, assertion.Value)
		}
	default:
		return fmt.Errorf("unsupported assertion operator %q", op)
	}
	return nil
}

func valuesEqual(a, b any) bool {
	if an, ok := numberValue(a); ok {
		if bn, ok := numberValue(b); ok {
			return an == bn
		}
	}
	return reflect.DeepEqual(a, b) || fmt.Sprint(a) == fmt.Sprint(b)
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func jsonPath(payload any, path string) (any, bool) {
	if path == "" || path == "$" {
		return payload, true
	}
	if !strings.HasPrefix(path, "$.") {
		return nil, false
	}
	current := payload
	parts := strings.Split(strings.TrimPrefix(path, "$."), ".")
	for _, part := range parts {
		field := part
		index := -1
		if open := strings.Index(part, "["); open >= 0 && strings.HasSuffix(part, "]") {
			field = part[:open]
			parsed, err := strconv.Atoi(strings.TrimSuffix(part[open+1:], "]"))
			if err != nil {
				return nil, false
			}
			index = parsed
		}
		if field != "" {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok = object[field]
			if !ok {
				return nil, false
			}
		}
		if index >= 0 {
			array, ok := current.([]any)
			if !ok || index >= len(array) {
				return nil, false
			}
			current = array[index]
		}
	}
	return current, true
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
