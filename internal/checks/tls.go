package checks

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type TLSChecker struct {
	Options Options
}

func (c TLSChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	host, port, err := hostPort(monitor.Target, "443")
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

	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: timeout},
		Config:    &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
	}
	start := time.Now()
	conn, err := dialer.DialContext(checkCtx, "tcp", net.JoinHostPort(host, port))
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(start).Milliseconds()
	result.TLSHandshakeMS = result.TotalMS
	result.ResponseTimeMS = result.TotalMS
	if err != nil {
		result.Error = fmt.Sprintf("tls handshake failed: %v", err)
		return result, err
	}
	defer conn.Close()

	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		err := fmt.Errorf("no peer certificate")
		result.Error = err.Error()
		return result, err
	}
	leaf := state.PeerCertificates[0]
	warnDays := c.Options.TLSExpiryWarnDays
	if warnDays == 0 {
		warnDays = 14
	}
	result.Success = true
	result.Status = TLSExpiryStatus(leaf.NotAfter, warnDays, time.Now())
	if result.Status == models.StatusDegraded {
		result.Error = fmt.Sprintf("certificate expires at %s", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	return result, nil
}

func TLSExpiryStatus(notAfter time.Time, warnDays int, now time.Time) models.CheckStatus {
	if warnDays <= 0 {
		warnDays = 14
	}
	if notAfter.Before(now) {
		return models.StatusDown
	}
	if notAfter.Sub(now) <= time.Duration(warnDays)*24*time.Hour {
		return models.StatusDegraded
	}
	return models.StatusUp
}
