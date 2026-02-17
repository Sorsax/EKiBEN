package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	LevelInfo    = "INF"
	LevelWarn    = "WRN"
	LevelError   = "ERR"
	LevelDebug   = "DBG"
	LevelTraffic = "TRF"
)

const (
	colorInfo    = "\x1b[90m" // dark gray
	colorWarn    = "\x1b[33m" // yellow
	colorError   = "\x1b[31m" // red
	colorDebug   = "\x1b[36m" // cyan
	colorTraffic = "\x1b[35m" // magenta
	colorAccent  = "\x1b[33m" // yellow
	colorReset   = "\x1b[0m"
)

// Logger provides clean, structured logging with optional traffic details
type Logger struct {
	out        io.Writer
	logTraffic bool
	useColor   bool
}

// New creates a new logger
func New(out io.Writer, logTraffic bool, useColor bool) *Logger {
	return &Logger{
		out:        out,
		logTraffic: logTraffic,
		useColor:   useColor,
	}
}

// Accent highlights important values when color is enabled.
func (l *Logger) Accent(text string) string {
	if !l.useColor {
		return text
	}
	return colorAccent + text + colorReset
}

// Infof logs an info message
func (l *Logger) Infof(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	prefix := l.prefix(LevelInfo, colorInfo)
	fmt.Fprintf(l.out, "%s %s\n", prefix, msg)
}

// Headerf logs a startup header message
func (l *Logger) Headerf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	prefix := l.prefix(LevelInfo, colorInfo)
	fmt.Fprintf(l.out, "%s %s\n", prefix, msg)
}

// Debugf logs a debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	prefix := l.prefix(LevelDebug, colorDebug)
	fmt.Fprintf(l.out, "%s %s\n", prefix, msg)
}

// Warnf logs a warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	prefix := l.prefix(LevelWarn, colorWarn)
	fmt.Fprintf(l.out, "%s %s\n", prefix, msg)
}

// Errorf logs an error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	prefix := l.prefix(LevelError, colorError)
	fmt.Fprintf(l.out, "%s %s\n", prefix, msg)
}

// Fatalf logs a fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	prefix := l.prefix(LevelError, colorError)
	fmt.Fprintf(l.out, "%s %s\n", prefix, msg)
}

// TrafficTx logs outgoing traffic (only if logTraffic is enabled)
func (l *Logger) TrafficTx(label string, payload interface{}) {
	if !l.logTraffic {
		return
	}
	l.trafficLog("TX", label, payload)
}

// TrafficRx logs incoming traffic (only if logTraffic is enabled)
func (l *Logger) TrafficRx(label string, data []byte) {
	if !l.logTraffic {
		return
	}
	const maxLen = 2000
	msg := string(data)
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "...<truncated>"
	}
	prefix := l.prefix(LevelTraffic, colorTraffic)
	fmt.Fprintf(l.out, "%s RX %s %s\n", prefix, label, msg)
}

// trafficLog logs traffic with JSON formatting
func (l *Logger) trafficLog(direction string, label string, payload interface{}) {
	var msg string

	switch v := payload.(type) {
	case []byte:
		msg = string(v)
	case string:
		msg = v
	default:
		data, _ := json.Marshal(payload)
		msg = string(data)
	}

	const maxLen = 2000
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "...<truncated>"
	}

	prefix := l.prefix(LevelTraffic, colorTraffic)
	fmt.Fprintf(l.out, "%s %s %s %s\n", prefix, direction, label, msg)
}

// timestamp returns formatted time
func (l *Logger) timestamp() string {
	return time.Now().Format("15:04:05")
}

func (l *Logger) prefix(level string, color string) string {
	base := fmt.Sprintf("[%s %s]", l.timestamp(), level)
	if !l.useColor {
		return base
	}
	return color + base + colorReset
}
