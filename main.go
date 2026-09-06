package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

type closeFunc func() error

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {
	debugHandler := slog.NewTextHandler(
		os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)

	if logFile == "" {
		return slog.New(debugHandler), func() error { return nil }, nil
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, func() error { return nil }, fmt.Errorf("error opening log file: %w", err)
	}

	bufferedFile := bufio.NewWriterSize(file, 8192)
	infoHandler := slog.NewJSONHandler(
		bufferedFile,
		&slog.HandlerOptions{
			Level: slog.LevelInfo,
		},
	)

	logger := slog.New(slog.NewMultiHandler(debugHandler, infoHandler))

	return logger, func() error {
		var err error

		if flushErr := bufferedFile.Flush(); flushErr != nil {
			err = fmt.Errorf("error flushing file: %w", flushErr)
		}

		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("error closing file: %w", closeErr))
		}

		return err
	}, nil
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	logger, closeLogger, err := initializeLogger(os.Getenv("LINKO_LOG_FILE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		return 1
	}
	defer func() {
		if err := closeLogger(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close logger: %v\n", err)
		}
	}()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error("failed to create store", slog.String("error", err.Error()))
		return 1
	}

	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Debug("Linko is shutting down")

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown server", slog.String("error", err.Error()))
		return 1
	}
	if serverErr != nil {
		logger.Error("server error", slog.String("error", serverErr.Error()))
		return 1
	}
	return 0
}
