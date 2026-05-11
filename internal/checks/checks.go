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

	"github.com/jusso-dev/uptime/internal/models"
)

const maxSnippetBytes = 4096

var ErrBlockedTarget = errors.New("target resolves to a private, loopback, link-local, or unspecified address")

type Checker interface {
	Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error)
}

type Options struct {
	AllowPrivateTargets bool
	DefaultTimeout      time.Duration
	UserAgent           string
	TLSExpiryWarnDays   int
}

type Registry struct {
	HTTP Checker
	TCP  Checker
	DNS  Checker
	TLS  Checker
}

func NewRegistry(opts Options) Registry {
	if opts.DefaultTimeout == 0 {
		opts.DefaultTimeout = 10 * time.Second
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "UpTime-Monitor/1.0"
	}
	return Registry{
		HTTP: HTTPChecker{Options: opts},
		TCP:  TCPChecker{Options: opts},
		DNS:  DNSChecker{Options: opts},
		TLS:  TLSChecker{Options: opts},
	}
}

func (r Registry) For(monitorType models.MonitorType) (Checker, error) {
	switch monitorType {
	case models.MonitorHTTP, models.MonitorKeyword:
		return r.HTTP, nil
	case models.MonitorTCP:
		return r.TCP, nil
	case models.MonitorDNS:
		return r.DNS, nil
	case models.MonitorTLS:
		return r.TLS, nil
	default:
		return nil, fmt.Errorf("unsupported monitor type %q", monitorType)
	}
}

func TimeoutFor(m models.Monitor, fallback time.Duration) time.Duration {
	if m.TimeoutSeconds > 0 {
		return time.Duration(m.TimeoutSeconds) * time.Second
	}
	if fallback > 0 {
		return fallback
	}
	return 10 * time.Second
}

func ValidateURL(raw string, allowPrivate bool) (*url.URL, error) {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url scheme must be http or https")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("url host is required")
	}
	if !allowPrivate {
		if err := validatePublicHost(context.Background(), u.Hostname()); err != nil {
			return nil, err
		}
	}
	return u, nil
}

func validatePublicHost(ctx context.Context, host string) error {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve target host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("target host did not resolve")
	}
	for _, ip := range ips {
		if !isPublicIP(ip.IP) {
			return ErrBlockedTarget
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified())
}

func hostPort(target, defaultPort string) (string, string, error) {
	value := strings.TrimSpace(target)
	if value == "" {
		return "", "", fmt.Errorf("target is required")
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			host = value
			port = defaultPort
		} else {
			return "", "", err
		}
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("target must include host and port")
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
