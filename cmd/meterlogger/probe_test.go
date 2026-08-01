package main

// The real serial paths of probeHeat and probeGrid (serialmbus.NewReader,
// gridmeter.ReadGridTelegrams opening the port) require hardware and are
// exercised here only up to the open failure. This follows the documented
// hardware-dependent test exemption.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func TestProbeFuncFor(t *testing.T) {
	for _, source := range []string{
		config.SourceHeat, config.SourceGrid, config.SourceSolar, config.SourceVentilation,
	} {
		t.Run(source, func(t *testing.T) {
			fn, err := probeFuncFor(source)
			if err != nil {
				t.Fatalf("probeFuncFor(%q) error = %v", source, err)
			}
			if fn == nil {
				t.Fatalf("probeFuncFor(%q) returned nil func", source)
			}
		})
	}

	t.Run("invalid source", func(t *testing.T) {
		if _, err := probeFuncFor("water"); err == nil {
			t.Error("probeFuncFor(\"water\") should return an error")
		}
	})
}

func TestProbe_NotConfigured(t *testing.T) {
	swapCfg(t, config.Config{})
	ctx := context.Background()
	l := testLogger()

	tests := []struct {
		name    string
		fn      probeFunc
		wantMsg string
	}{
		{config.SourceHeat, probeHeat, "heat source not configured"},
		{config.SourceGrid, probeGrid, "grid source not configured"},
		{config.SourceSolar, probeSolar, "solar source not configured"},
		{config.SourceVentilation, probeVentilation, "ventilation source not configured"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.fn(ctx, l)
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want containing %q", err, tt.wantMsg)
			}
		})
	}
}

func TestMissingEnphaseFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.EnphaseConfig
		want []string
	}{
		{"all empty", config.EnphaseConfig{}, []string{"EnvoyURL", "User", "Password", "Serial"}},
		{
			"all set",
			config.EnphaseConfig{EnvoyURL: "http://envoy", User: "u", Password: "p", Serial: "s"},
			nil,
		},
		{
			"serial missing",
			config.EnphaseConfig{EnvoyURL: "http://envoy", User: "u", Password: "p"},
			[]string{"Serial"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingEnphaseFields(tt.cfg)
			if len(got) != len(tt.want) {
				t.Fatalf("missingEnphaseFields() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("missingEnphaseFields()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// fakeGridTelegramReader implements domain.GridTelegramReader for tests. It
// mirrors the real reader's contract: the telegram channel is closed when
// ReadGridTelegrams returns.
type fakeGridTelegramReader struct {
	telegrams chan domain.GridTelegram
	deliver   []domain.GridTelegram
	err       error
}

func newFakeGridTelegramReader(deliver []domain.GridTelegram, err error) *fakeGridTelegramReader {
	return &fakeGridTelegramReader{
		telegrams: make(chan domain.GridTelegram),
		deliver:   deliver,
		err:       err,
	}
}

func (f *fakeGridTelegramReader) Telegrams() <-chan domain.GridTelegram { return f.telegrams }

func (f *fakeGridTelegramReader) ReadGridTelegrams(ctx context.Context) error {
	defer close(f.telegrams)
	for _, telegram := range f.deliver {
		select {
		case f.telegrams <- telegram:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestReadOneGridTelegram(t *testing.T) {
	ctx := context.Background()

	t.Run("returns the first telegram", func(t *testing.T) {
		want := domain.GridTelegram{Serienummer: "S123", TotalPowerUsage: 42}
		reader := newFakeGridTelegramReader([]domain.GridTelegram{want, {Serienummer: "later"}}, nil)
		got, err := readOneGridTelegram(ctx, reader)
		if err != nil {
			t.Fatalf("readOneGridTelegram() error = %v", err)
		}
		if got.Serienummer != want.Serienummer || got.TotalPowerUsage != want.TotalPowerUsage {
			t.Errorf("readOneGridTelegram() = %+v, want %+v", got, want)
		}
	})

	t.Run("propagates reader error", func(t *testing.T) {
		wantErr := errors.New("serial port gone")
		reader := newFakeGridTelegramReader(nil, wantErr)
		if _, err := readOneGridTelegram(ctx, reader); !errors.Is(err, wantErr) {
			t.Errorf("readOneGridTelegram() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("reader stops without telegram or error", func(t *testing.T) {
		reader := newFakeGridTelegramReader(nil, nil)
		_, err := readOneGridTelegram(ctx, reader)
		if err == nil || !strings.Contains(err.Error(), "stopped before delivering") {
			t.Errorf("readOneGridTelegram() error = %v, want stopped-before-delivering", err)
		}
	})

	t.Run("context deadline surfaces", func(t *testing.T) {
		deadlineCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
		defer cancel()
		reader := newFakeGridTelegramReader(nil, nil)
		<-deadlineCtx.Done()
		if _, err := readOneGridTelegram(deadlineCtx, reader); err == nil {
			t.Error("readOneGridTelegram() with expired context should return an error")
		}
	})
}

// ducoTestServer serves minimal DucoBox API responses for the ventilation
// probe.
func ducoTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/boxinfoget":
			_, _ = w.Write([]byte(`{"General":{"Time":1}}`))
		case "/nodeinfoget":
			_, _ = w.Write([]byte(`{"devtype":"BOX","node":1,"trgt":50,"actl":45}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProbeVentilation(t *testing.T) {
	srv := ducoTestServer(t)
	ctx := context.Background()
	l := testLogger()

	t.Run("box only", func(t *testing.T) {
		swapCfg(t, config.Config{Ventilation: config.VentilationConfig{HostURL: srv.URL}})
		raw, err := probeVentilation(ctx, l)
		if err != nil {
			t.Fatalf("probeVentilation() error = %v", err)
		}
		var got map[string]json.RawMessage
		if unmarshalErr := json.Unmarshal(raw, &got); unmarshalErr != nil {
			t.Fatalf("probe output is not valid JSON: %v", unmarshalErr)
		}
		if _, ok := got["Box"]; !ok {
			t.Error("probe output missing Box")
		}
		if _, ok := got["Node"]; ok {
			t.Error("probe output should omit Node when no nodes are configured")
		}
	})

	t.Run("box and first node", func(t *testing.T) {
		swapCfg(t, config.Config{
			Ventilation: config.VentilationConfig{HostURL: srv.URL, Nodes: []int{1, 2}},
		})
		raw, err := probeVentilation(ctx, l)
		if err != nil {
			t.Fatalf("probeVentilation() error = %v", err)
		}
		var got map[string]json.RawMessage
		if unmarshalErr := json.Unmarshal(raw, &got); unmarshalErr != nil {
			t.Fatalf("probe output is not valid JSON: %v", unmarshalErr)
		}
		if _, ok := got["Node"]; !ok {
			t.Error("probe output missing Node")
		}
	})

	t.Run("unreachable box", func(t *testing.T) {
		swapCfg(t, config.Config{
			Ventilation: config.VentilationConfig{HostURL: "http://127.0.0.1:1"},
		})
		if _, err := probeVentilation(ctx, l); err == nil {
			t.Error("probeVentilation() against unreachable box should return an error")
		}
	})
}

func TestRunProbe(t *testing.T) {
	l := testLogger()
	ctx := context.Background()

	t.Run("invalid source", func(t *testing.T) {
		var out strings.Builder
		if got := runProbe(ctx, "water", time.Second, &out, l); got != 1 {
			t.Errorf("runProbe(water) = %d, want 1", got)
		}
		if out.Len() != 0 {
			t.Errorf("stdout should stay empty on failure, got %q", out.String())
		}
	})

	t.Run("unconfigured source fails", func(t *testing.T) {
		swapCfg(t, config.Config{})
		var out strings.Builder
		if got := runProbe(ctx, config.SourceGrid, time.Second, &out, l); got != 1 {
			t.Errorf("runProbe(grid) = %d, want 1", got)
		}
	})

	t.Run("success prints indented JSON", func(t *testing.T) {
		srv := ducoTestServer(t)
		swapCfg(t, config.Config{Ventilation: config.VentilationConfig{HostURL: srv.URL}})
		var out strings.Builder
		if got := runProbe(ctx, config.SourceVentilation, 5*time.Second, &out, l); got != 0 {
			t.Fatalf("runProbe(ventilation) = %d, want 0", got)
		}
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil {
			t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, out.String())
		}
		if !strings.Contains(out.String(), "\n  ") {
			t.Error("output is not indented")
		}
	})
}
