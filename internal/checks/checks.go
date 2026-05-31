// Package checks implements the synthetic checkers (HTTP, TCP, DNS, TLS,
// Heartbeat, ICMP, Domain, Multistep, Browser). Each Checker is stateless
// and safe for concurrent use; per-check state lives in a CheckResult
// returned to the caller.
package checks

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jusso-dev/uptime/internal/models"
)

// maxSnippetBytes is the maximum amount of response body bytes captured for
// keyword matching and operator debugging. Anything larger would bloat
// check_results rows.
const maxSnippetBytes = 4096

// ErrBlockedTarget is returned when an SSRF guard rejects a target that
// resolves to a non-public address. Using a sentinel error lets callers
// distinguish policy violations from network errors with errors.Is.
var ErrBlockedTarget = errors.New("target resolves to a private, loopback, link-local, or unspecified address")

type Checker interface {
	Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error)
}

// Options is shared configuration for every checker. Fields are read-only
// after construction; Registry copies the struct into each checker.
type Options struct {
	AllowPrivateTargets bool
	DefaultTimeout      time.Duration
	UserAgent           string
	TLSExpiryWarnDays   int

	// HeartbeatStore is required for heartbeat checks; nil disables them.
	HeartbeatStore HeartbeatStore
	// MultistepStore is required for multi-step checks.
	MultistepStore MultistepStore
	// BrowserStore is required for browser checks.
	BrowserStore BrowserStore
	// Redis client used to dispatch browser jobs to the Node sidecar.
	Redis *redis.Client
	// BrowserEnabled gates browser-check execution; allows operators to
	// keep the schema/migrations in place without standing up the sidecar.
	BrowserEnabled bool
	// ICMPEnabled gates ICMP-check execution; allows operators on hosts
	// that block ICMP to keep the migration without spurious failures.
	ICMPEnabled bool
}

// Registry bundles concrete checkers so the rest of the codebase can pick
// the right one for a given monitor type without depending on each
// implementation directly.
type Registry struct {
	HTTP      Checker
	TCP       Checker
	UDP       Checker
	DNS       Checker
	TLS       Checker
	Heartbeat Checker
	ICMP      Checker
	Ping      Checker
	Domain    Checker
	Multistep Checker
	Browser   Checker
}

func NewRegistry(opts Options) Registry {
	if opts.DefaultTimeout <= 0 {
		opts.DefaultTimeout = 10 * time.Second
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "UpTime-Monitor/1.0"
	}
	if opts.TLSExpiryWarnDays <= 0 {
		opts.TLSExpiryWarnDays = 14
	}
	return Registry{
		HTTP:      HTTPChecker{Options: opts},
		TCP:       TCPChecker{Options: opts},
		UDP:       UDPChecker{Options: opts},
		DNS:       DNSChecker{Options: opts},
		TLS:       TLSChecker{Options: opts},
		Heartbeat: HeartbeatChecker{Options: opts, Store: opts.HeartbeatStore},
		ICMP:      ICMPChecker{Options: opts},
		Ping:      PingChecker{Options: opts},
		Domain:    DomainChecker{Options: opts},
		Multistep: MultistepChecker{Options: opts, Store: opts.MultistepStore},
		Browser:   BrowserChecker{Options: opts, Store: opts.BrowserStore, Redis: opts.Redis, Enabled: opts.BrowserEnabled},
	}
}

func (r Registry) For(monitorType models.MonitorType) (Checker, error) {
	switch monitorType {
	case models.MonitorHTTP, models.MonitorKeyword, models.MonitorAPI:
		return r.HTTP, nil
	case models.MonitorTCP:
		return r.TCP, nil
	case models.MonitorUDP:
		return r.UDP, nil
	case models.MonitorDNS:
		return r.DNS, nil
	case models.MonitorTLS:
		return r.TLS, nil
	case models.MonitorHeartbeat:
		return r.Heartbeat, nil
	case models.MonitorICMP:
		return r.ICMP, nil
	case models.MonitorPing:
		return r.Ping, nil
	case models.MonitorDomain:
		return r.Domain, nil
	case models.MonitorMultistep:
		return r.Multistep, nil
	case models.MonitorBrowser:
		return r.Browser, nil
	default:
		return nil, fmt.Errorf("unsupported monitor type %q", monitorType)
	}
}

// TimeoutFor returns the effective check timeout, preferring the monitor's
// own setting, then the package-level fallback, then a final 10s safety net.
func TimeoutFor(m models.Monitor, fallback time.Duration) time.Duration {
	if m.TimeoutSeconds > 0 {
		return time.Duration(m.TimeoutSeconds) * time.Second
	}
	if fallback > 0 {
		return fallback
	}
	return 10 * time.Second
}

// ValidateURL parses a target URL, enforces the http(s) scheme, and (unless
// allowPrivate is true) ensures every resolved IP is publicly routable. The
// resolution check here is a best-effort SSRF guard; the per-request dialer
// re-checks at connect time to defend against DNS rebinding.
func ValidateURL(raw string, allowPrivate bool) (*url.URL, error) {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url scheme %q must be http or https", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("url host is required")
	}
	if !allowPrivate {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := validatePublicHost(ctx, u.Hostname()); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func validatePublicHost(ctx context.Context, host string) error {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve target host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("target host %q did not resolve", host)
	}
	for _, ip := range ips {
		if !isPublicIP(ip.IP) {
			return ErrBlockedTarget
		}
	}
	return nil
}

// isPublicIP returns true only for addresses that are safe to connect to
// from a public-facing checker (no loopback, RFC1918, link-local, or
// "unspecified" zero addresses).
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !(ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsUnspecified())
}

func hostPort(target, defaultPort string) (string, string, error) {
	value := strings.TrimSpace(target)
	if value == "" {
		return "", "", fmt.Errorf("target is required")
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		// SplitHostPort returns a typed addr error; the cleanest portable
		// check is the substring it always uses.
		if strings.Contains(err.Error(), "missing port in address") && defaultPort != "" {
			host = value
			port = defaultPort
		} else {
			return "", "", fmt.Errorf("parse target %q: %w", value, err)
		}
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("target %q must include host and port", value)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return "", "", fmt.Errorf("invalid port %q: %w", port, err)
	}
	return host, port, nil
}

func baseResult(m models.Monitor) models.CheckResult {
	return models.CheckResult{
		MonitorID:  m.ID,
		Status:     models.StatusDown,
		CheckedAt:  time.Now().UTC(),
		StatusCode: 0,
	}
}

// traceTimings collects per-phase HTTP timings via httptrace callbacks.
// Fields are written from goroutines owned by the net/http transport and
// read by the caller after Do returns, so there is no concurrent access.
type traceTimings struct {
	start             time.Time
	dnsStart          time.Time
	connectStart      time.Time
	tlsStart          time.Time
	wroteRequest      time.Time
	dnsMS             int64
	tcpConnectMS      int64
	tlsHandshakeMS    int64
	timeToFirstByteMS int64
}

func (t *traceTimings) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			t.dnsStart = time.Now()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			t.dnsMS = elapsedMS(t.dnsStart, time.Now())
		},
		ConnectStart: func(_, _ string) {
			t.connectStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			t.tcpConnectMS = elapsedMS(t.connectStart, time.Now())
		},
		TLSHandshakeStart: func() {
			t.tlsStart = time.Now()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			t.tlsHandshakeMS = elapsedMS(t.tlsStart, time.Now())
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			t.wroteRequest = time.Now()
		},
		GotFirstResponseByte: func() {
			t.timeToFirstByteMS = elapsedMS(t.wroteRequest, time.Now())
		},
	}
}

func elapsedMS(start, end time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
