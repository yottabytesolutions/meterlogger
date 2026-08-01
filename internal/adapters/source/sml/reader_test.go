package sml

// Serial port opening (openPort, the non-portReader branch of
// ReadGridTelegrams) requires real hardware and is intentionally untested;
// tests inject portReader instead.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// runReader starts ReadGridTelegrams on src, collects every delivered
// telegram, and returns them with the reader's final error.
func runReader(ctx context.Context, t *testing.T, src io.Reader) ([]domain.GridTelegram, error) {
	t.Helper()
	r := NewReader("/dev/null", testLogger())
	r.portReader = src
	readErr := make(chan error, 1)
	go func() { readErr <- r.ReadGridTelegrams(ctx) }()
	var telegrams []domain.GridTelegram
	for telegram := range r.Telegrams() {
		telegrams = append(telegrams, telegram)
	}
	return telegrams, <-readErr
}

func TestReaderStreamsTelegrams(t *testing.T) {
	frame := buildFrame(t, fullPayload(t), crcVariantX25)
	stream := append(mustHex(t, "00AB1B1BFF"), frame...)
	stream = append(stream, frame...)

	telegrams, err := runReader(context.Background(), t, bytes.NewReader(stream))
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected wrapped io.EOF at stream end, got %v", err)
	}
	if len(telegrams) != 2 {
		t.Fatalf("got %d telegrams, want 2", len(telegrams))
	}
	if telegrams[0].MeterMerkType != "ISK" {
		t.Errorf("MeterMerkType = %q, want ISK", telegrams[0].MeterMerkType)
	}
	if telegrams[0].Time.IsZero() {
		t.Error("telegram Time is zero, want receive time")
	}
}

func TestReaderSkipsBadFrames(t *testing.T) {
	good := buildFrame(t, fullPayload(t), crcVariantX25)

	corrupted := append([]byte{}, good...)
	corrupted[12] ^= 0xFF // breaks the CRC

	// Valid frame whose payload has no 1-0:1.8.0 entry.
	unparseable := buildFrame(t, mustHex(t, "77070100100700FF"+"01"+"01"+"621B"+"5200"+"530FA0"), crcVariantX25)

	stream := append(append([]byte{}, corrupted...), unparseable...)
	stream = append(stream, good...)

	telegrams, err := runReader(context.Background(), t, bytes.NewReader(stream))
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected wrapped io.EOF at stream end, got %v", err)
	}
	if len(telegrams) != 1 {
		t.Fatalf("got %d telegrams, want 1 (bad frames skipped)", len(telegrams))
	}
}

func TestReaderKermitVariant(t *testing.T) {
	frame := buildFrame(t, fullPayload(t), crcVariantKermit)
	telegrams, err := runReader(context.Background(), t, bytes.NewReader(frame))
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected wrapped io.EOF at stream end, got %v", err)
	}
	if len(telegrams) != 1 {
		t.Fatalf("got %d telegrams, want 1", len(telegrams))
	}
}

func TestReaderContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	telegrams, err := runReader(ctx, t, bytes.NewReader(nil))
	if err != nil {
		t.Errorf("expected nil error on cancelled context, got %v", err)
	}
	if len(telegrams) != 0 {
		t.Errorf("got %d telegrams, want 0", len(telegrams))
	}
}
