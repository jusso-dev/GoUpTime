package checks

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type PingChecker struct {
	Options Options
}

func (c PingChecker) Check(ctx context.Context, monitor models.Monitor) (models.CheckResult, error) {
	result := baseResult(monitor)
	host := monitor.Target
	if parsedHost, _, err := net.SplitHostPort(monitor.Target); err == nil {
		host = parsedHost
	}
	if host == "" {
		err := fmt.Errorf("ping target host is required")
		result.Error = err.Error()
		return result, err
	}
	if !c.Options.AllowPrivateTargets {
		if err := validatePublicHost(ctx, host); err != nil {
			result.Error = err.Error()
			return result, err
		}
	}
	if mode, _ := monitor.Config["mode"].(string); mode != "tcp" {
		if checked, done := c.icmpPing(ctx, monitor, host); done {
			return checked, nil
		}
	}
	return c.tcpPing(ctx, monitor, host)
}

func (c PingChecker) icmpPing(ctx context.Context, monitor models.Monitor, host string) (models.CheckResult, bool) {
	result := baseResult(monitor)
	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ipAddr, err := net.DefaultResolver.LookupIPAddr(checkCtx, host)
	if err != nil || len(ipAddr) == 0 {
		return result, false
	}
	ip := ipAddr[0].IP.To4()
	if ip == nil {
		return result, false
	}
	start := time.Now()
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return result, false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{ID: int(time.Now().UnixNano() & 0xffff), Seq: 1, Data: []byte("uptime")},
	}
	b, err := msg.Marshal(nil)
	if err != nil {
		return result, false
	}
	if _, err := conn.WriteTo(b, &net.IPAddr{IP: ip}); err != nil {
		return result, false
	}
	reply := make([]byte, 1500)
	n, _, err := conn.ReadFrom(reply)
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(start).Milliseconds()
	result.ResponseTimeMS = result.TotalMS
	if err != nil {
		return result, false
	}
	parsed, err := icmp.ParseMessage(1, reply[:n])
	if err != nil || parsed.Type != ipv4.ICMPTypeEchoReply {
		return result, false
	}
	result.Success = true
	result.Status = models.StatusUp
	result.Metadata = map[string]any{"mode": "icmp"}
	return result, true
}

func (c PingChecker) tcpPing(ctx context.Context, monitor models.Monitor, host string) (models.CheckResult, error) {
	result := baseResult(monitor)
	port := "443"
	if value, ok := monitor.Config["fallbackPort"].(string); ok && value != "" {
		port = value
	}
	if value, ok := monitor.Config["port"].(string); ok && value != "" {
		port = value
	}
	timeout := TimeoutFor(monitor, c.Options.DefaultTimeout)
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	var dialer net.Dialer
	conn, err := dialer.DialContext(checkCtx, "tcp", net.JoinHostPort(host, port))
	result.CheckedAt = time.Now().UTC()
	result.TotalMS = time.Since(start).Milliseconds()
	result.TCPConnectMS = result.TotalMS
	result.ResponseTimeMS = result.TotalMS
	if err != nil {
		result.Error = fmt.Sprintf("tcp ping to %s:%s failed: %v", host, port, err)
		result.Metadata = map[string]any{"mode": "tcp", "port": port}
		return result, err
	}
	_ = conn.Close()
	result.Success = true
	result.Status = models.StatusUp
	result.Metadata = map[string]any{"mode": "tcp", "port": port}
	return result, nil
}
