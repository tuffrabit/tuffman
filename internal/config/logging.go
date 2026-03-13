package config

import (
	"fmt"
	"io"
	"log"
	"os"
)

// LogLevel represents the logging level
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// ParseLogLevel parses a log level string
func ParseLogLevel(level string) LogLevel {
	switch level {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// String returns the string representation of a log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "debug"
	case LogLevelInfo:
		return "info"
	case LogLevelWarn:
		return "warn"
	case LogLevelError:
		return "error"
	default:
		return "unknown"
	}
}

// Logger is a configured logger that respects the config settings
type Logger struct {
	level  LogLevel
	format string
	debug  *log.Logger
	info   *log.Logger
	warn   *log.Logger
	error  *log.Logger
}

// NewLogger creates a new logger from config
func (c *Config) NewLogger() *Logger {
	level := ParseLogLevel(c.Logging.Level)
	format := c.Logging.Format
	if format == "" {
		format = "text"
	}

	return &Logger{
		level:  level,
		format: format,
		debug:  log.New(os.Stderr, "[DEBUG] ", log.Ltime),
		info:   log.New(os.Stderr, "[INFO] ", log.Ltime),
		warn:   log.New(os.Stderr, "[WARN] ", log.Ltime),
		error:  log.New(os.Stderr, "[ERROR] ", log.Ltime),
	}
}

// Debug logs a debug message
func (l *Logger) Debug(format string, v ...interface{}) {
	if l.level <= LogLevelDebug {
		l.debug.Output(2, fmt.Sprintf(format, v...))
	}
}

// Info logs an info message
func (l *Logger) Info(format string, v ...interface{}) {
	if l.level <= LogLevelInfo {
		l.info.Output(2, fmt.Sprintf(format, v...))
	}
}

// Warn logs a warning message
func (l *Logger) Warn(format string, v ...interface{}) {
	if l.level <= LogLevelWarn {
		l.warn.Output(2, fmt.Sprintf(format, v...))
	}
}

// Error logs an error message
func (l *Logger) Error(format string, v ...interface{}) {
	if l.level <= LogLevelError {
		l.error.Output(2, fmt.Sprintf(format, v...))
	}
}

// SetOutput sets the output writer for all log levels
func (l *Logger) SetOutput(w io.Writer) {
	l.debug.SetOutput(w)
	l.info.SetOutput(w)
	l.warn.SetOutput(w)
	l.error.SetOutput(w)
}

// GetLogLevel returns the configured log level
func (c *Config) GetLogLevel() LogLevel {
	return ParseLogLevel(c.Logging.Level)
}

// GetLogFormat returns the configured log format
func (c *Config) GetLogFormat() string {
	if c.Logging.Format == "" {
		return "text"
	}
	return c.Logging.Format
}
