package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const logContextKey contextKey = "log_context"

type LogContext struct {
	Username string
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			logCtx := &LogContext{}

			ctx := context.WithValue(r.Context(), logContextKey, logCtx)
			r = r.WithContext(ctx)

			spyReader := &spyReadCloser{ReadCloser: r.Body}
			r.Body = spyReader

			spyWriter := &spyResponseWriter{ResponseWriter: w}

			h.ServeHTTP(spyWriter, r)

			dur := time.Since(start)
			status := spyWriter.statusCode
			if status == 0 {
				status = http.StatusOK
			}

			reqLogger := logger
			if logCtx.Username != "" {
				reqLogger = reqLogger.With("user", logCtx.Username)
			}

			reqLogger.Info("Served request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("client_ip", r.RemoteAddr),

				slog.Duration("duration", dur),
				slog.Int("request_body_bytes", spyReader.bytesRead),
				slog.Int("response_status", status),
				slog.Int("response_body_bytes", spyWriter.bytesWritten),
			)
		})
	}
}

type spyReadCloser struct {
	io.ReadCloser
	bytesRead int
}

func (r *spyReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += n
	return n, err
}

type spyResponseWriter struct {
	http.ResponseWriter
	bytesWritten int
	statusCode   int
}

func (s *spyResponseWriter) Write(p []byte) (int, error) {
	if s.statusCode == 0 {
		s.statusCode = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(p)
	s.bytesWritten += n
	return n, err
}

func (s *spyResponseWriter) WriteHeader(statusCode int) {
	s.statusCode = statusCode
	s.ResponseWriter.WriteHeader(statusCode)
}
