package checks

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

func TestTCPCheckerSuccessAndFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	checker := TCPChecker{Options: Options{AllowPrivateTargets: true, DefaultTimeout: time.Second}}
	result, err := checker.Check(context.Background(), models.Monitor{Type: models.MonitorTCP, Target: listener.Addr().String()})
	if err != nil || !result.Success {
		t.Fatalf("expected tcp success result=%+v err=%v", result, err)
	}

	result, err = checker.Check(context.Background(), models.Monitor{Type: models.MonitorTCP, Target: "127.0.0.1:1", TimeoutSeconds: 1})
	if err == nil || result.Success {
		t.Fatalf("expected tcp failure result=%+v err=%v", result, err)
	}
}

func TestDNSCheckerSuccessAndFailure(t *testing.T) {
	checker := DNSChecker{Options: Options{DefaultTimeout: time.Second}}
	result, err := checker.Check(context.Background(), models.Monitor{Type: models.MonitorDNS, Target: "localhost"})
	if err != nil || !result.Success {
		t.Fatalf("expected dns success result=%+v err=%v", result, err)
	}

	result, err = checker.Check(context.Background(), models.Monitor{Type: models.MonitorDNS, Target: "invalid.invalid."})
	if err == nil || result.Success {
		t.Fatalf("expected dns failure result=%+v err=%v", result, err)
	}
}
