package main

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const logContextKey contextKey = "log_context"

type LogContext struct {
	Username string
	Error    error
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-REQUEST-ID")
		if id == "" {
			id = rand.Text()
		}
		w.Header().Set("X-REQUEST-ID", id)
		next.ServeHTTP(w, r)
	})
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

			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("client_ip", r.RemoteAddr),

				slog.String("request_id", w.Header().Get("X-REQUEST-ID")),

				slog.Duration("duration", dur),
				slog.Int("request_body_bytes", spyReader.bytesRead),
				slog.Int("response_status", status),
				slog.Int("response_body_bytes", spyWriter.bytesWritten),
			}

			if logCtx.Username != "" {
				attrs = append(attrs, slog.String("user", logCtx.Username))
			}
			if logCtx.Error != nil {
				attrs = append(attrs, "error", logCtx.Error)
			}

			logger.Info("Served request", attrs...)
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

func httpError(ctx context.Context, w http.ResponseWriter, status int, err error) {
	if logCtx, ok := ctx.Value(logContextKey).(*LogContext); ok {
		logCtx.Error = err
	}
	http.Error(w, err.Error(), status)
}
