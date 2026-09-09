package main

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"boot.dev/linko/internal/build"
	"boot.dev/linko/internal/linkoerr"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	pkgerr "github.com/pkg/errors"
)

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

type multiError interface {
	error
	Unwrap() []error
}

// errorAttrs describes a single error: its message, a stack trace when one is
// available, and any attributes attached along the unwrap chain.
func errorAttrs(err error) []slog.Attr {
	attrs := []slog.Attr{slog.String("message", err.Error())}

	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(attrs, slog.String("stack_trace", fmt.Sprintf("%+v", stackErr.StackTrace())))
	}

	return append(attrs, linkoerr.Attrs(err)...)
}

func replaceAttr(groups []string, attr slog.Attr) slog.Attr {
	if attr.Key != "error" {
		return attr
	}

	err, ok := attr.Value.Any().(error)
	if !ok {
		return attr
	}

	if multiErr, ok := errors.AsType[multiError](err); ok {
		errs := multiErr.Unwrap()
		attrs := make([]slog.Attr, 0, len(errs))
		for i, e := range errs {
			attrs = append(attrs, slog.GroupAttrs(fmt.Sprintf("error_%d", i+1), errorAttrs(e)...))
		}
		return slog.GroupAttrs("errors", attrs...)
	}

	return slog.GroupAttrs("error", errorAttrs(err)...)
}

type closeFunc func() error

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {
	isTerminal := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	disableColor := !isTerminal

	debugHandler := tint.NewTextHandler(os.Stderr, &tint.Options{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
		NoColor:     disableColor,
	})

	env := os.Getenv("ENV")
	if env == "" {
		env = "unknown"
	}
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get hostname: %v\n", err)
	}

	extraAttrs := []any{
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	}

	if logFile == "" {
		return slog.New(debugHandler).With(extraAttrs...), func() error { return nil }, nil
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

	logger := slog.New(slog.NewMultiHandler(debugHandler, infoHandler)).With(extraAttrs...)

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
