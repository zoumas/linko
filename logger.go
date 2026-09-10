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
	var (
		handlers []slog.Handler
		closers  []closeFunc
	)

	closeAll := func() error {
		var errs []error
		for _, closer := range closers {
			errs = append(errs, closer())
		}
		return errors.Join(errs...)
	}

	// console: everything, coloured only when stderr is a terminal
	isTerminal := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	handlers = append(handlers, tint.NewTextHandler(os.Stderr, &tint.Options{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
		NoColor:     !isTerminal,
	}))

	env := os.Getenv("ENV")
	if env == "" {
		env = "unknown"
	}
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get hostname: %v\n", err)
		hostname = "unknown"
	}

	extraAttrs := []any{
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	}

	// file: info and above, as JSON, buffered
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, closeAll, fmt.Errorf("error opening log file: %w", err)
		}

		bufferedFile := bufio.NewWriterSize(file, 8192)
		handlers = append(handlers, slog.NewJSONHandler(
			bufferedFile,
			&slog.HandlerOptions{
				Level:       slog.LevelInfo,
				ReplaceAttr: replaceAttr,
			},
		))
		closers = append(closers, func() error {
			var err error

			if flushErr := bufferedFile.Flush(); flushErr != nil {
				err = fmt.Errorf("error flushing file: %w", flushErr)
			}

			if closeErr := file.Close(); closeErr != nil {
				err = errors.Join(err, fmt.Errorf("error closing file: %w", closeErr))
			}

			return err
		})
	}

	logger := slog.New(slog.NewMultiHandler(handlers...)).With(extraAttrs...)

	return logger, closeAll, nil
}
