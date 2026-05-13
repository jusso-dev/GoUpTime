package worker

import (
	"testing"
	"time"
)

func TestJitterBoundedBySpan(t *testing.T) {
	d := time.Second
	for i := 0; i < 1000; i++ {
		got := jitter(d, 0.1)
		if got < -d/10 || got > d/10 {
			t.Fatalf("jitter %v out of [-%v, +%v]", got, d/10, d/10)
		}
	}
}

func TestJitterZeroForZeroDuration(t *testing.T) {
	if got := jitter(0, 0.5); got != 0 {
		t.Fatalf("expected 0 jitter for zero duration, got %v", got)
	}
}

func TestJitterClampsFractionAboveOne(t *testing.T) {
	d := time.Second
	for i := 0; i < 200; i++ {
		got := jitter(d, 5.0)
		if got < -d || got > d {
			t.Fatalf("jitter %v out of [-%v, +%v]", got, d, d)
		}
	}
}
