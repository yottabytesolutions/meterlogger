package service

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestDeviceReadAvailabilityClassification(t *testing.T) {
	for _, tt := range []struct {
		name      string
		err       error
		temporary bool
	}{
		{"source timeout", deviceReadError{context.DeadlineExceeded}, true},
		{"source network reset", deviceReadError{&net.OpError{Op: "read", Net: "tcp", Err: errors.New("reset")}}, true},
		{"sink timeout", context.DeadlineExceeded, false},
		{"device authentication", deviceReadError{errors.New("HTTP 401")}, false},
		{"malformed device data", deviceReadError{errors.New("invalid JSON")}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := transientDeviceRead(tt.err); got != tt.temporary {
				t.Errorf("temporary=%v, want %v", got, tt.temporary)
			}
		})
	}
}

func TestSolarRecoversAfterExtendedTemporaryDeviceOutage(t *testing.T) {
	reader := &mockSolarReader{readErr: context.DeadlineExceeded}
	sink := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, sink, time.Second, time.Second, testLogger())
	count := 0
	for range maxConsecutiveErrors * 3 {
		if svc.handleTick(context.Background(), &count) {
			t.Fatal("temporary device outage stopped service")
		}
	}
	if len(sink.stored) != 0 {
		t.Fatal("unavailable device wrote fabricated data")
	}
	reader.readErr = nil
	if svc.handleTick(context.Background(), &count) {
		t.Fatal("recovery stopped service")
	}
	if len(sink.stored) != 1 {
		t.Fatalf("stored %d rows after recovery", len(sink.stored))
	}
	sink.storeErr = context.DeadlineExceeded
	if svc.handleTick(context.Background(), &count) {
		t.Fatal("first sink timeout should retain existing threshold")
	}
	if count != 1 {
		t.Fatalf("sink timeout did not count towards failure threshold: %d", count)
	}
}

func TestDucoRecoversAfterExtendedTemporaryDeviceOutage(t *testing.T) {
	reader := &mockDucoReader{boxErr: context.DeadlineExceeded}
	sink := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, sink, time.Second, time.Second, nil, testLogger())
	count := 0
	for range maxErrorCount * 3 {
		if svc.handleRead(context.Background(), &count) {
			t.Fatal("temporary device outage stopped service")
		}
	}
	if len(sink.storedBox) != 0 {
		t.Fatal("unavailable device wrote fabricated data")
	}
	reader.boxErr = nil
	if svc.handleRead(context.Background(), &count) {
		t.Fatal("recovery stopped service")
	}
	if len(sink.storedBox) != 1 {
		t.Fatalf("stored %d rows after recovery", len(sink.storedBox))
	}
	sink.storeBoxErr = context.DeadlineExceeded
	if svc.handleRead(context.Background(), &count) {
		t.Fatal("first sink timeout should retain existing threshold")
	}
	if count != 1 {
		t.Fatalf("sink timeout did not count towards failure threshold: %d", count)
	}
}
