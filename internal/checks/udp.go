package checks

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

type UDPChecker struct {
	Options Options
}

func (c UDPChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	host, port, err := hostPort(monitor.Target, "")
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

	payload := []byte("ping")
	if value, ok := monitor.Config["payload"].(string); ok {
		payload = []byte(value)
	}
	expected, _ := monitor.Config["expectedResponse"].(string)
	start := time.Now()
	var dialer net.Dialer
	conn, err := dialer.DialContext(checkCtx, "udp", net.JoinHostPort(host, port))
	result.CheckedAt = time.Now().UTC()
	if err != nil {
		result.TotalMS = time.Since(start).Milliseconds()
		result.ResponseTimeMS = result.TotalMS
		result.Error = fmt.Sprintf("udp dial to %s:%s failed: %v", host, port, err)
		return result, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(payload); err != nil {
		result.TotalMS = time.Since(start).Milliseconds()
		result.ResponseTimeMS = result.TotalMS
		result.Error = fmt.Sprintf("udp write to %s:%s failed: %v", host, port, err)
		return result, err
	}
	if expected != "" {
		buf := make([]byte, maxSnippetBytes)
		n, err := conn.Read(buf)
		result.TotalMS = time.Since(start).Milliseconds()
		result.ResponseTimeMS = result.TotalMS
		if err != nil {
			result.Error = fmt.Sprintf("udp read from %s:%s failed: %v", host, port, err)
			return result, err
		}
		result.ResponseSnippet = string(buf[:n])
		if !strings.Contains(result.ResponseSnippet, expected) {
			result.Error = fmt.Sprintf("expected UDP response containing %q", expected)
			return result, nil
		}
	}
	result.TotalMS = time.Since(start).Milliseconds()
	result.ResponseTimeMS = result.TotalMS
	result.Success = true
	result.Status = models.StatusUp
	return result, nil
}
