package applog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxLogSize  = 2 << 20
	maxMessage  = 8 << 10
	currentName = "expand.log"
	backupName  = "expand.log.1"
)

var (
	credentialPattern = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|authorization|cookie|api[_-]?key)\b\s*[:=]\s*[^\s,;]+`)
	dsnPattern        = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/\s]+:)[^@\s]+(@)`)
)

type Logger struct {
	mu   sync.Mutex
	file *os.File
	path string
	out  io.Writer
}

func New() (*Logger, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve log directory: %w", err)
	}
	logDir := filepath.Join(cacheRoot, "expand", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	path := filepath.Join(logDir, currentName)
	if info, statErr := os.Stat(path); statErr == nil && info.Size() >= maxLogSize {
		backup := filepath.Join(logDir, backupName)
		_ = os.Remove(backup)
		_ = os.Rename(path, backup)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return &Logger{file: file, path: path, out: file}, nil
}

func Discard() *Logger {
	return &Logger{out: io.Discard}
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logger) Info(event, message string) {
	l.write("INFO", event, message)
}

func (l *Logger) Error(event string, err error) {
	if err == nil {
		return
	}
	l.write("ERROR", event, err.Error())
}

func (l *Logger) Frontend(level, message, stack string) {
	level = strings.ToUpper(strings.TrimSpace(level))
	if level != "WARN" && level != "ERROR" {
		level = "INFO"
	}
	text := strings.TrimSpace(message)
	if strings.TrimSpace(stack) != "" {
		text += "\n" + strings.TrimSpace(stack)
	}
	l.write(level, "frontend", text)
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	l.out = io.Discard
	return err
}

func (l *Logger) write(level, event, message string) {
	if l == nil {
		return
	}
	event = safeLine(event)
	message = redact(message)
	if len(message) > maxMessage {
		message = message[:maxMessage] + "…[truncated]"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.out == nil {
		return
	}
	timestamp := time.Now().Format(time.RFC3339Nano)
	_, _ = fmt.Fprintf(l.out, "%s level=%s event=%s message=%q\n", timestamp, level, event, message)
	if l.file != nil {
		_ = l.file.Sync()
	}
}

func safeLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			return '_'
		}
		return r
	}, value)
	if value == "" {
		return "unknown"
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}

func redact(value string) string {
	value = credentialPattern.ReplaceAllString(value, "$1=[REDACTED]")
	return dsnPattern.ReplaceAllString(value, "$1[REDACTED]$2")
}
