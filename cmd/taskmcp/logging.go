package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
	errors "gitlab.com/tozd/go/errors"
)

// logWriter implements io.Writer by writing to a zerolog.Logger
type logWriter struct {
	logger zerolog.Logger
}

// Write implements io.Writer
func (w *logWriter) Write(p []byte) (n int, err error) {
	// Remove trailing newlines and spaces for cleaner log output
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		w.logger.Error().Msg(msg)
	}
	return len(p), nil
}

// loggerMiddleware wraps an HTTP handler with request/response logging
func loggerMiddleware(next http.Handler, logger zerolog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Extract request details for logging
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = fmt.Sprintf("%d", time.Now().UnixNano())
		}

		// Create logger with request context
		reqLogger := logger.With().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Str("content_type", r.Header.Get("Content-Type")).
			Logger()

		// Only log bodies for specific content types
		shouldLogBody := false
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(contentType, "application/json") ||
			strings.Contains(contentType, "text/plain") ||
			strings.Contains(contentType, "text/html") ||
			strings.Contains(contentType, "application/x-www-form-urlencoded") {
			shouldLogBody = true
		}

		// Max size for body logging (8KB)
		const maxBodyLogSize = 8 * 1024

		// Capture request body if appropriate
		var requestBody []byte
		if r.Body != nil && shouldLogBody {
			// Read up to maxBodyLogSize bytes
			requestBody, _ = io.ReadAll(io.LimitReader(r.Body, maxBodyLogSize))
			// Restore the body for further processing
			r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
		}

		// Log request with body
		reqEvent := reqLogger.Info().Str("phase", "request")
		if len(requestBody) > 0 {
			// Try to pretty-print JSON bodies
			if strings.Contains(contentType, "application/json") {
				reqEvent.RawJSON("body", requestBody)
			} else {
				// For non-JSON, just log as string but limit size
				if len(requestBody) > maxBodyLogSize {
					reqEvent.Str("body", string(requestBody[:maxBodyLogSize])+"... [truncated]")
				} else {
					reqEvent.Str("body", string(requestBody))
				}
			}
		}
		reqEvent.Msg("Received request")

		// Add request ID to context
		ctx := r.Context()
		ctx = context.WithValue(ctx, "requestID", requestID)
		r = r.WithContext(ctx)

		// Create a custom response writer to capture the response
		rw := newLoggingResponseWriter(w)

		// Process the request
		next.ServeHTTP(rw, r)

		// Log the response with body
		duration := time.Since(start)
		respEvent := reqLogger.Info().
			Str("phase", "response").
			Int("status", rw.statusCode).
			Int("size", rw.size).
			Str("content_type", rw.Header().Get("Content-Type")).
			Dur("duration", duration)

		// Include response body if captured and not too large
		respContentType := rw.Header().Get("Content-Type")
		shouldLogRespBody := strings.Contains(respContentType, "application/json") ||
			strings.Contains(respContentType, "text/plain") ||
			strings.Contains(respContentType, "text/html")

		if len(rw.body) > 0 && shouldLogRespBody {
			// Limit response body logging
			bodyToLog := rw.body
			if len(bodyToLog) > maxBodyLogSize {
				bodyToLog = bodyToLog[:maxBodyLogSize]
				respEvent.Bool("truncated", true)
			}

			// Try to pretty-print JSON bodies
			if strings.Contains(respContentType, "application/json") {
				respEvent.RawJSON("body", bodyToLog)
			} else {
				respEvent.Str("body", string(bodyToLog))
			}
		}

		respEvent.Msg("Request completed")
	})
}

// loggingResponseWriter is a custom ResponseWriter that captures the status code, size and body
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	size        int
	body        []byte
	bodyCapture bool
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK, // Default to 200 OK
		bodyCapture:    true,          // Enable body capture by default
	}
}

// WriteHeader implements http.ResponseWriter
func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code

	// Decide if we should capture the body based on content type and size
	contentType := lrw.Header().Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") ||
		strings.Contains(contentType, "application/octet-stream") {
		// Don't capture bodies for streaming responses
		lrw.bodyCapture = false
	}

	lrw.ResponseWriter.WriteHeader(code)
}

// Write implements http.ResponseWriter
func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	// Capture the response body if enabled and not too large
	const maxCaptureSize = 32 * 1024 // 32KB max total capture
	if lrw.bodyCapture && len(lrw.body) < maxCaptureSize {
		bytesToCapture := b
		remainingSpace := maxCaptureSize - len(lrw.body)
		if len(b) > remainingSpace {
			bytesToCapture = b[:remainingSpace]
		}
		lrw.body = append(lrw.body, bytesToCapture...)
	}

	size, err := lrw.ResponseWriter.Write(b)
	lrw.size += size
	return size, err
}

// Flush implements http.Flusher
func (lrw *loggingResponseWriter) Flush() {
	if flusher, ok := lrw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Push implements http.Pusher for HTTP/2 support
func (lrw *loggingResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := lrw.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

// CloseNotify implements http.CloseNotifier (deprecated but sometimes still used)
func (lrw *loggingResponseWriter) CloseNotify() <-chan bool {
	if cn, ok := lrw.ResponseWriter.(http.CloseNotifier); ok {
		return cn.CloseNotify()
	}
	// If the underlying ResponseWriter doesn't implement CloseNotifier, return a never-closing channel
	ch := make(chan bool, 1)
	return ch
}

// Hijack implements http.Hijacker
func (lrw *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := lrw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("http.Hijacker not supported")
}

func init() {
	// Configure zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
}
