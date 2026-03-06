package device

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// Log Level
// =============================================================================

// LogLevel controls the severity threshold for log output.
// Matches Kotlin/Swift SDKs and runanywhere-commons log levels.
type LogLevel int

const (
	LogLevelTrace   LogLevel = 0
	LogLevelDebug   LogLevel = 1
	LogLevelInfo    LogLevel = 2
	LogLevelWarning LogLevel = 3
	LogLevelError   LogLevel = 4
	LogLevelFault   LogLevel = 5
)

// String returns the human-readable name for the log level.
func (l LogLevel) String() string {
	switch l {
	case LogLevelTrace:
		return "TRACE"
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarning:
		return "WARNING"
	case LogLevelError:
		return "ERROR"
	case LogLevelFault:
		return "FAULT"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// =============================================================================
// Log Category
// =============================================================================

// LogCategory is a typed string for log subsystems.
type LogCategory string

const (
	LogCategoryCore       LogCategory = "Core"
	LogCategoryLLM        LogCategory = "LLM"
	LogCategorySTT        LogCategory = "STT"
	LogCategoryTTS        LogCategory = "TTS"
	LogCategoryEmbeddings LogCategory = "Embeddings"
	LogCategoryNetwork    LogCategory = "Network"
	LogCategoryBridge     LogCategory = "Bridge"
	LogCategoryModels     LogCategory = "Models"
)

// =============================================================================
// Log Entry
// =============================================================================

// LogEntry is a structured log record delivered to LogDestination implementations.
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Category  LogCategory
	Message   string
	Metadata  map[string]string // sanitized before delivery
	Err       error             // optional wrapped error
}

// =============================================================================
// Log Destination Interface
// =============================================================================

// LogDestination receives log entries. Implement this interface to bridge to
// zerolog, zap, slog, or any other logging framework.
type LogDestination interface {
	WriteEntry(entry LogEntry)
}

// =============================================================================
// No-Op Log Destination
// =============================================================================

// NoOpLogDestination discards all entries.
type NoOpLogDestination struct{}

// WriteEntry is a no-op.
func (NoOpLogDestination) WriteEntry(LogEntry) {}

// =============================================================================
// Standard Log Destination
// =============================================================================

// StdLogDestination writes to the standard log package, preserving the behavior
// of the original log.Printf calls.
type StdLogDestination struct{}

// WriteEntry formats and writes the entry to the standard logger.
func (StdLogDestination) WriteEntry(entry LogEntry) {
	if entry.Err != nil {
		log.Printf("[%s] [%s] %s: %v", entry.Level, entry.Category, entry.Message, entry.Err)
	} else {
		log.Printf("[%s] [%s] %s", entry.Level, entry.Category, entry.Message)
	}
}

// =============================================================================
// Global Log Destination (same pattern as metrics.go)
// =============================================================================

var (
	logMu          sync.RWMutex
	globalLogDest  LogDestination = StdLogDestination{}
	globalLogLevel LogLevel       = LogLevelInfo
)

// SetLogDestination sets the global log destination.
// Pass nil to reset to StdLogDestination.
func SetLogDestination(d LogDestination) {
	if d == nil {
		d = StdLogDestination{}
	}
	logMu.Lock()
	defer logMu.Unlock()
	globalLogDest = d
}

// GetLogDestination returns the current global log destination.
func GetLogDestination() LogDestination {
	logMu.RLock()
	defer logMu.RUnlock()
	return globalLogDest
}

// SetLogLevel sets the global log level threshold.
func SetLogLevel(level LogLevel) {
	logMu.Lock()
	defer logMu.Unlock()
	globalLogLevel = level
}

// GetLogLevel returns the current global log level threshold.
func GetLogLevel() LogLevel {
	logMu.RLock()
	defer logMu.RUnlock()
	return globalLogLevel
}

// =============================================================================
// Logger (category-scoped convenience)
// =============================================================================

// Logger provides category-scoped logging convenience methods.
type Logger struct {
	category LogCategory
}

// NewLogger creates a Logger scoped to the given category.
func NewLogger(category LogCategory) *Logger {
	return &Logger{category: category}
}

// Trace logs a message at TRACE level.
func (l *Logger) Trace(msg string, metadata ...map[string]string) {
	l.emit(LogLevelTrace, msg, nil, metadata)
}

// Debug logs a message at DEBUG level.
func (l *Logger) Debug(msg string, metadata ...map[string]string) {
	l.emit(LogLevelDebug, msg, nil, metadata)
}

// Info logs a message at INFO level.
func (l *Logger) Info(msg string, metadata ...map[string]string) {
	l.emit(LogLevelInfo, msg, nil, metadata)
}

// Warn logs a message at WARNING level.
func (l *Logger) Warn(msg string, metadata ...map[string]string) {
	l.emit(LogLevelWarning, msg, nil, metadata)
}

// Error logs a message at ERROR level with an optional error.
func (l *Logger) Error(msg string, err error, metadata ...map[string]string) {
	l.emit(LogLevelError, msg, err, metadata)
}

// Fault logs a message at FAULT level with an optional error.
func (l *Logger) Fault(msg string, err error, metadata ...map[string]string) {
	l.emit(LogLevelFault, msg, err, metadata)
}

func (l *Logger) emit(level LogLevel, msg string, err error, metadata []map[string]string) {
	logMu.RLock()
	threshold := globalLogLevel
	dest := globalLogDest
	logMu.RUnlock()

	if level < threshold {
		return
	}

	var merged map[string]string
	for _, m := range metadata {
		if len(m) == 0 {
			continue
		}
		if merged == nil {
			merged = make(map[string]string, len(m))
		}
		for k, v := range m {
			merged[k] = v
		}
	}

	sanitizeMetadata(merged)

	dest.WriteEntry(LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Category:  l.category,
		Message:   msg,
		Metadata:  merged,
		Err:       err,
	})
}

// =============================================================================
// Package-Level Convenience Loggers
// =============================================================================

var (
	LogCore       = NewLogger(LogCategoryCore)
	LogLLM        = NewLogger(LogCategoryLLM)
	LogSTT        = NewLogger(LogCategorySTT)
	LogTTS        = NewLogger(LogCategoryTTS)
	LogEmbeddings = NewLogger(LogCategoryEmbeddings)
	LogNetwork    = NewLogger(LogCategoryNetwork)
	LogBridge     = NewLogger(LogCategoryBridge)
	LogModels     = NewLogger(LogCategoryModels)
)

// =============================================================================
// Metadata Sanitization
// =============================================================================

// sensitiveKeys are substrings (case-insensitive) that indicate a metadata
// value should be redacted. Matches Kotlin/Swift SDK behavior.
var sensitiveKeys = []string{"key", "secret", "password", "token", "auth", "credential"}

func sanitizeMetadata(m map[string]string) {
	for k := range m {
		lower := strings.ToLower(k)
		for _, s := range sensitiveKeys {
			if strings.Contains(lower, s) {
				m[k] = "[REDACTED]"
				break
			}
		}
	}
}
