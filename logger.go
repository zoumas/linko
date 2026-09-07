package main

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"

	pkgerr "github.com/pkg/errors"
)

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

func replaceAttr(groups []string, attr slog.Attr) slog.Attr {
	if attr.Key != "error" {
		return attr
	}

	err, ok := attr.Value.Any().(error)
	if !ok {
		return attr
	}

	stackErr, ok := errors.AsType[stackTracer](err)
	if !ok {
		return slog.String("error", fmt.Sprintf("%+v", err))
	}

	return slog.GroupAttrs("error",
		slog.String("message", stackErr.Error()),
		slog.String("stack_trace", fmt.Sprintf("%+v", stackErr.StackTrace())),
	)
}

type closeFunc func() error

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {
	debugHandler := slog.NewTextHandler(
		os.Stderr,
		&slog.HandlerOptions{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
		},
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
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceAttr,
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
