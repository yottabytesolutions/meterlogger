package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yottabytesolutions/meterlogger/internal/service"
)

func interruptAwareContext() context.Context {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
	)
	go func() {
		<-ctx.Done()
		logger.Info("received interrupt signal")
		stop()
	}()

	return ctx
}

// doWork runs the service function in a loop, restarting it if it exits
// before the context is cancelled (e.g. on transient errors signalled via processKiller).
func doWork(ctx context.Context, l *slog.Logger, serviceName string, svc func(context.Context)) {
	for {
		select {
		case <-ctx.Done():
			l.InfoContext(ctx, "received context cancellation from service", slog.String("service", serviceName))
			return
		default:
			l.InfoContext(ctx, "starting service", slog.String("service", serviceName))
			svc(ctx)
			l.InfoContext(ctx, "service finished", slog.String("service", serviceName))
		}
	}
}

func startService(ctx context.Context, l *slog.Logger, name string, s service.Service) {
	doWork(ctx, l, name, s.Start)
}
