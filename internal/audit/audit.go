package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/user/clone/internal/tracing"
	"go.uber.org/zap"
)

// AuditEventType represents the type of audit event
type AuditEventType string

const (
	AuditEventCrawlStart       AuditEventType = "crawl_start"
	AuditEventCrawlStop        AuditEventType = "crawl_stop"
	AuditEventCrawlPause       AuditEventType = "crawl_pause"
	AuditEventCrawlResume      AuditEventType = "crawl_resume"
	AuditEventPageFetched      AuditEventType = "page_fetched"
	AuditEventPageError        AuditEventType = "page_error"
	AuditEventAssetDownloaded  AuditEventType = "asset_downloaded"
	AuditEventAssetError       AuditEventType = "asset_error"
	AuditEventConfigChange     AuditEventType = "config_change"
	AuditEventAuthAttempt      AuditEventType = "auth_attempt"
	AuditEventAuthSuccess      AuditEventType = "auth_success"
	AuditEventAuthFailure      AuditEventType = "auth_failure"
	AuditEventPluginLoad       AuditEventType = "plugin_load"
	AuditEventPluginExecute    AuditEventType = "plugin_execute"
	AuditEventDataExport       AuditEventType = "data_export"
	AuditEventConfigLoad       AuditEventType = "config_load"
	AuditEventSecretsAccess    AuditEventType = "secrets_access"
	AuditEventAPIRequest       AuditEventType = "api_request"
)

// AuditEvent represents a single audit log entry
type AuditEvent struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Type      AuditEventType         `json:"type"`
	Actor     string                 `json:"actor"`      // User, API key, system component
	Action    string                 `json:"action"`     // What was done
	Resource  string                 `json:"resource"`   // What resource was affected
	Outcome   string                 `json:"outcome"`    // success, failure, partial
	Details   map[string]interface{} `json:"details"`    // Additional context
	Error     string                 `json:"error,omitempty"`
	Duration  time.Duration          `json:"duration,omitempty"`
	IP        string                 `json:"ip,omitempty"`
	UserAgent string                 `json:"user_agent,omitempty"`
	TraceID   string                 `json:"trace_id,omitempty"`
	SpanID    string                 `json:"span_id,omitempty"`
}

// AuditLogger is the interface for audit logging
type AuditLogger interface {
	Log(ctx context.Context, event *AuditEvent) error
	Query(ctx context.Context, filter AuditFilter) ([]*AuditEvent, error)
	Close() error
}

// AuditFilter represents filters for querying audit logs
type AuditFilter struct {
	StartTime  *time.Time
	EndTime    *time.Time
	Types      []AuditEventType
	Actors     []string
	Resources  []string
	Outcomes   []string
	Limit      int
	Offset     int
}

// FileAuditLogger writes audit events to JSON Lines files
type FileAuditLogger struct {
	mu          sync.Mutex
	file        *os.File
	writer      *json.Encoder
	dir         string
	currentDate string
	maxFileSize int64
	currentSize int64
	logger      *zap.Logger
}

// NewFileAuditLogger creates a new file-based audit logger
func NewFileAuditLogger(dir string, maxFileSize int64) (*FileAuditLogger, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	logger := &FileAuditLogger{
		dir:         dir,
		maxFileSize: maxFileSize,
		logger:      zap.L().Named("audit"),
	}

	if err := logger.rotate(); err != nil {
		return nil, err
	}

	return logger, nil
}

// rotate creates a new log file for the current date
func (f *FileAuditLogger) rotate() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.file != nil {
		f.file.Close()
	}

	date := time.Now().Format("2006-01-02")
	if date != f.currentDate {
		f.currentDate = date
		f.currentSize = 0
	}

	filename := filepath.Join(f.dir, fmt.Sprintf("audit-%s.log", f.currentDate))
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}

	f.file = file
	f.writer = json.NewEncoder(file)

	// Write header
	header := map[string]string{
		"version": "1.0",
		"type":    "audit_log",
		"date":    date,
	}
	if err := f.writer.Encode(header); err != nil {
		return err
	}

	return nil
}

// Log writes an audit event
func (f *FileAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	// Add trace context if available
	if span := tracing.SpanFromContext(ctx); span != nil {
		spanCtx := span.SpanContext()
		if spanCtx.IsValid() {
			event.TraceID = spanCtx.TraceID().String()
			event.SpanID = spanCtx.SpanID().String()
		}
	}

	if event.ID == "" {
		event.ID = fmt.Sprintf("audit-%d-%s", time.Now().UnixNano(), randomString(8))
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if we need to rotate
	if f.currentSize > f.maxFileSize {
		if err := f.rotate(); err != nil {
			return err
		}
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if _, err := f.file.WriteString(string(data) + "\n"); err != nil {
		return err
	}

	f.currentSize += int64(len(data)) + 1

	// Also log to structured logger
	f.logger.Info("audit event",
		zap.String("event_id", event.ID),
		zap.String("type", string(event.Type)),
		zap.String("actor", event.Actor),
		zap.String("action", event.Action),
		zap.String("resource", event.Resource),
		zap.String("outcome", event.Outcome),
		zap.String("trace_id", event.TraceID),
	)

	return nil
}

// Query queries audit events from log files
func (f *FileAuditLogger) Query(ctx context.Context, filter AuditFilter) ([]*AuditEvent, error) {
	// This is a simplified implementation - in production, use a proper log aggregation system
	var events []*AuditEvent
	
	files, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".log" {
			continue
		}

		filePath := filepath.Join(f.dir, file.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var event AuditEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}

			if f.matchesFilter(event, filter) {
				events = append(events, &event)
				if filter.Limit > 0 && len(events) >= filter.Limit {
					break
				}
			}
		}

		if filter.Limit > 0 && len(events) >= filter.Limit {
			break
		}
	}

	return events, nil
}

func (f *FileAuditLogger) matchesFilter(event AuditEvent, filter AuditFilter) bool {
	if filter.StartTime != nil && event.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && event.Timestamp.After(*filter.EndTime) {
		return false
	}
	if len(filter.Types) > 0 {
		found := false
		for _, t := range filter.Types {
			if event.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Actors) > 0 {
		found := false
		for _, a := range filter.Actors {
			if event.Actor == a {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Resources) > 0 {
		found := false
		for _, r := range filter.Resources {
			if event.Resource == r {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Outcomes) > 0 {
		found := false
		for _, o := range filter.Outcomes {
			if event.Outcome == o {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Close closes the audit logger
func (f *FileAuditLogger) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.file != nil {
		return f.file.Close()
	}
	return nil
}

// SyslogAuditLogger writes audit events to syslog (placeholder)
type SyslogAuditLogger struct {
	// Implementation would connect to syslog
}

func NewSyslogAuditLogger() (*SyslogAuditLogger, error) {
	return &SyslogAuditLogger{}, nil
}

func (s *SyslogAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	// TODO: Implement syslog writing
	return nil
}

func (s *SyslogAuditLogger) Query(ctx context.Context, filter AuditFilter) ([]*AuditEvent, error) {
	return nil, fmt.Errorf("syslog query not supported")
}

func (s *SyslogAuditLogger) Close() error {
	return nil
}

// AuditMiddleware provides HTTP middleware for audit logging
type AuditMiddleware struct {
	logger AuditLogger
}

func NewAuditMiddleware(logger AuditLogger) *AuditMiddleware {
	return &AuditMiddleware{logger: logger}
}

func (m *AuditMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Wrap response writer to capture status
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		next.ServeHTTP(wrapped, r)
		
		duration := time.Since(start)
		
		// Log API request
		event := &AuditEvent{
			Type:      AuditEventAPIRequest,
			Actor:     getClientIP(r),
			Action:    r.Method,
			Resource:  r.URL.Path,
			Outcome:   fmt.Sprintf("status_%d", wrapped.statusCode),
			Duration:  duration,
			IP:        getClientIP(r),
			UserAgent: r.UserAgent(),
			Details: map[string]interface{}{
				"method":      r.Method,
				"path":        r.URL.Path,
				"query":       r.URL.RawQuery,
				"status_code": wrapped.statusCode,
			},
		}
		
		if err := m.logger.Log(r.Context(), event); err != nil {
			// Log error but don't fail the request
			zap.L().Error("failed to log audit event", zap.Error(err))
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}
	// Check X-Real-IP header
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	// Fall back to remote address
	return r.RemoteAddr
}

// AuditHelper provides convenient methods for common audit events
type AuditHelper struct {
	logger AuditLogger
}

func NewAuditHelper(logger AuditLogger) *AuditHelper {
	return &AuditHelper{logger: logger}
}

func (h *AuditHelper) LogCrawlStart(ctx context.Context, actor, configHash string) error {
	return h.logger.Log(ctx, &AuditEvent{
		Type:     AuditEventCrawlStart,
		Actor:    actor,
		Action:   "start_crawl",
		Resource: "crawl",
		Outcome:  "success",
		Details: map[string]interface{}{
			"config_hash": configHash,
		},
	})
}

func (h *AuditHelper) LogCrawlStop(ctx context.Context, actor string, pagesFetched, assetsDownloaded int, duration time.Duration) error {
	return h.logger.Log(ctx, &AuditEvent{
		Type:     AuditEventCrawlStop,
		Actor:    actor,
		Action:   "stop_crawl",
		Resource: "crawl",
		Outcome:  "success",
		Duration: duration,
		Details: map[string]interface{}{
			"pages_fetched":    pagesFetched,
			"assets_downloaded": assetsDownloaded,
		},
	})
}

func (h *AuditHelper) LogPageFetched(ctx context.Context, actor, url string, statusCode int, duration time.Duration, size int64) error {
	return h.logger.Log(ctx, &AuditEvent{
		Type:     AuditEventPageFetched,
		Actor:    actor,
		Action:   "fetch_page",
		Resource: url,
		Outcome:  fmt.Sprintf("status_%d", statusCode),
		Duration: duration,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"size_bytes":  size,
		},
	})
}

func (h *AuditHelper) LogAuthAttempt(ctx context.Context, actor, authType, target string, success bool) error {
	outcome := "success"
	if !success {
		outcome = "failure"
	}
	return h.logger.Log(ctx, &AuditEvent{
		Type:     AuditEventAuthAttempt,
		Actor:    actor,
		Action:   "authenticate",
		Resource: target,
		Outcome:  outcome,
		Details: map[string]interface{}{
			"auth_type": authType,
			"target":    target,
			"success":   success,
		},
	})
}

func (h *AuditHelper) LogConfigChange(ctx context.Context, actor, configPath string, oldConfig, newConfig interface{}) error {
	return h.logger.Log(ctx, &AuditEvent{
		Type:     AuditEventConfigChange,
		Actor:    actor,
		Action:   "modify_config",
		Resource: configPath,
		Outcome:  "success",
		Details: map[string]interface{}{
			"old_config": oldConfig,
			"new_config": newConfig,
		},
	})
}

func (h *AuditHelper) LogDataExport(ctx context.Context, actor, format, path string, recordCount int) error {
	return h.logger.Log(ctx, &AuditEvent{
		Type:     AuditEventDataExport,
		Actor:    actor,
		Action:   "export_data",
		Resource: path,
		Outcome:  "success",
		Details: map[string]interface{}{
			"format":        format,
			"record_count":  recordCount,
		},
	})
}

func (h *AuditHelper) LogSecretsAccess(ctx context.Context, actor, secretPath string) error {
	return h.logger.Log(ctx, &AuditEvent{
		Type:     AuditEventSecretsAccess,
		Actor:    actor,
		Action:   "access_secret",
		Resource: secretPath,
		Outcome:  "success",
		Details: map[string]interface{}{
			"secret_path": secretPath,
		},
	})
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}