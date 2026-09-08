package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func Test_requestLogger(t *testing.T) {
	logBuffer := &bytes.Buffer{}

	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Time(slog.TimeKey, time.Date(2023, 10, 1, 12, 34, 57, 0, time.UTC))
			}
			// the measured duration differs on every run; pin it so the
			// rendered line is comparable
			if a.Key == "duration" {
				return slog.Duration("duration", 0)
			}
			// requestID generates a fresh id per request; pin it too
			if a.Key == "request_id" {
				return slog.String("request_id", "test-request-id")
			}
			return a
		},
	}))

	requestLoggerMiddleware := requestLogger(logger)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	loggedHandler := requestLoggerMiddleware(requestID(dummyHandler))

	req := httptest.NewRequest("GET", "http://lin.ko/api/stats", nil)
	rr := httptest.NewRecorder()
	loggedHandler.ServeHTTP(rr, req)

	const wantLogString = `time=2023-10-01T12:34:57.000Z level=INFO msg="Served request" method=GET path=/api/stats client_ip=192.0.2.1:1234 request_id=test-request-id duration=0s request_body_bytes=0 response_status=200 response_body_bytes=0` + "\n"
	const wantStatusCode = http.StatusOK

	if got := logBuffer.String(); got != wantLogString {
		t.Errorf("log output: got %q, want %q", got, wantLogString)
	}

	if got := rr.Code; got != wantStatusCode {
		t.Errorf("status code: got %d, want %d", got, wantStatusCode)
	}

	// the logged request_id is normalized above, so assert on the header
	// itself: requestID must generate an id and echo it to the client
	if got := rr.Header().Get("X-Request-ID"); got == "" {
		t.Error("response header X-Request-ID: got empty, want a generated id")
	}
}
