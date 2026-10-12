package observability

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var (
	// Redaction patterns
	sensitiveKeyPattern = regexp.MustCompile(`(?i)(api[-_]?key|secret|password|token|bearer|auth|database_url|db_url)`)
	phonePattern        = regexp.MustCompile(`\b\d{10,15}\b`)
)

// RedactingHandler wraps slog.Handler to sanitize sensitive keys and values
type RedactingHandler struct {
	handler slog.Handler
}

// NewRedactingHandler creates a new RedactingHandler
func NewRedactingHandler(h slog.Handler) *RedactingHandler {
	return &RedactingHandler{handler: h}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	newRecord := slog.NewRecord(r.Time, r.Level, RedactString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newRecord.AddAttrs(sanitizeAttr(a))
		return true
	})
	return h.handler.Handle(ctx, newRecord)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		sanitized[i] = sanitizeAttr(a)
	}
	return &RedactingHandler{handler: h.handler.WithAttrs(sanitized)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{handler: h.handler.WithGroup(name)}
}

func sanitizeAttr(a slog.Attr) slog.Attr {
	if sensitiveKeyPattern.MatchString(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}

	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, RedactString(a.Value.String()))
	case slog.KindGroup:
		attrs := a.Value.Group()
		sanitized := make([]slog.Attr, len(attrs))
		for i, sub := range attrs {
			sanitized[i] = sanitizeAttr(sub)
		}
		return slog.Group(a.Key, anySliceToAny(sanitized)...)
	default:
		return a
	}
}

func anySliceToAny(attrs []slog.Attr) []any {
	res := make([]any, len(attrs))
	for i, v := range attrs {
		res[i] = v
	}
	return res
}

// RedactString redacts sensitive strings like credentials and full phone numbers
func RedactString(s string) string {
	if len(s) == 0 {
		return s
	}

	// Redact phone numbers partially (e.g. 628123456789 -> 62812***789)
	s = phonePattern.ReplaceAllStringFunc(s, func(match string) string {
		if len(match) <= 6 {
			return match
		}
		prefix := match[:4]
		suffix := match[len(match)-3:]
		return prefix + "***" + suffix
	})

	// Redact URLs containing userinfo or secrets
	if strings.Contains(s, "://") && strings.Contains(s, "@") {
		idx := strings.Index(s, "://")
		atIdx := strings.Index(s[idx+3:], "@")
		if atIdx != -1 {
			s = s[:idx+3] + "[REDACTED]@" + s[idx+3+atIdx+1:]
		}
	}

	return s
}

// SetupLogger initializes the global structured logger
func SetupLogger(levelStr string) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	jsonHandler := slog.NewJSONHandler(os.Stdout, opts)
	redactingHandler := NewRedactingHandler(jsonHandler)
	logger := slog.New(redactingHandler)
	slog.SetDefault(logger)
	return logger
}
