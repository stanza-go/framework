// Package log provides structured JSON logging with level filtering, child
// loggers, and file rotation. It is built entirely on Go's standard library
// with zero external dependencies.
//
// Basic usage:
//
//	logger := log.New(log.WithLevel(log.LevelDebug))
//	logger.Info("server started", log.String("addr", ":8080"), log.Int("port", 8080))
//
// Output:
//
//	{"time":"2024-01-15T10:30:00.123Z","level":"info","msg":"server started","addr":":8080","port":8080}
//
// Child loggers carry pre-set fields:
//
//	httpLog := logger.With(log.String("component", "http"))
//	httpLog.Info("request", log.String("method", "GET"), log.String("path", "/api/v1"))
//
// File output with rotation:
//
//	fw, err := log.NewFileWriter("/var/log/myapp")
//	logger := log.New(log.WithWriter(io.MultiWriter(os.Stdout, fw)))
package log

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

const timeFormat = "2006-01-02T15:04:05.000Z"

const hexDigits = "0123456789abcdef"

var bufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 512))
	},
}

// Logger writes structured JSON log entries to a writer. It is safe for
// concurrent use by multiple goroutines.
type Logger struct {
	fields []Field
	w      io.Writer
	mu     *sync.Mutex
	level  Level
}

type config struct {
	fields []Field
	writer io.Writer
	level  Level
}

// Option configures a Logger.
type Option func(*config)

// WithLevel sets the minimum log level. Entries below this level are discarded.
// The default level is LevelInfo.
func WithLevel(level Level) Option {
	return func(c *config) {
		c.level = level
	}
}

// WithWriter sets the output destination. Defaults to os.Stdout.
func WithWriter(w io.Writer) Option {
	return func(c *config) {
		c.writer = w
	}
}

// WithFields sets fields that are included in every log entry.
func WithFields(fields ...Field) Option {
	return func(c *config) {
		c.fields = append(c.fields, fields...)
	}
}

// New creates a Logger with the given options.
func New(opts ...Option) *Logger {
	cfg := config{
		writer: os.Stdout,
		level:  LevelInfo,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Logger{
		w:      cfg.writer,
		mu:     &sync.Mutex{},
		level:  cfg.level,
		fields: cfg.fields,
	}
}

// With returns a new Logger that includes the given fields in every entry.
// The child logger shares the parent's writer and mutex, so writes are
// serialized across parent and all children.
func (l *Logger) With(fields ...Field) *Logger {
	combined := make([]Field, len(l.fields)+len(fields))
	copy(combined, l.fields)
	copy(combined[len(l.fields):], fields)
	return &Logger{
		fields: combined,
		w:      l.w,
		mu:     l.mu,
		level:  l.level,
	}
}

// Debug logs at LevelDebug.
func (l *Logger) Debug(msg string, fields ...Field) {
	l.log(LevelDebug, msg, fields)
}

// Info logs at LevelInfo.
func (l *Logger) Info(msg string, fields ...Field) {
	l.log(LevelInfo, msg, fields)
}

// Warn logs at LevelWarn.
func (l *Logger) Warn(msg string, fields ...Field) {
	l.log(LevelWarn, msg, fields)
}

// Error logs at LevelError.
func (l *Logger) Error(msg string, fields ...Field) {
	l.log(LevelError, msg, fields)
}

func (l *Logger) log(level Level, msg string, fields []Field) {
	if level < l.level {
		return
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()

	buf.WriteString(`{"time":"`)
	buf.WriteString(time.Now().UTC().Format(timeFormat))
	buf.WriteString(`","level":"`)
	buf.WriteString(level.String())
	buf.WriteString(`","msg":`)
	appendJSONString(buf, msg)

	for i := range l.fields {
		buf.WriteByte(',')
		appendJSONString(buf, l.fields[i].Key)
		buf.WriteByte(':')
		appendJSONValue(buf, l.fields[i].Value)
	}

	for i := range fields {
		buf.WriteByte(',')
		appendJSONString(buf, fields[i].Key)
		buf.WriteByte(':')
		appendJSONValue(buf, fields[i].Value)
	}

	buf.WriteString("}\n")

	l.mu.Lock()
	_, _ = l.w.Write(buf.Bytes())
	l.mu.Unlock()

	bufPool.Put(buf)
}

func appendJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' {
				buf.WriteByte(b)
				i++
				continue
			}
			switch b {
			case '"':
				buf.WriteString(`\"`)
			case '\\':
				buf.WriteString(`\\`)
			case '\n':
				buf.WriteString(`\n`)
			case '\r':
				buf.WriteString(`\r`)
			case '\t':
				buf.WriteString(`\t`)
			default:
				buf.WriteString(`\u00`)
				buf.WriteByte(hexDigits[b>>4])
				buf.WriteByte(hexDigits[b&0x0f])
			}
			i++
		} else {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				buf.WriteString(`\ufffd`)
			} else {
				buf.WriteString(s[i : i+size])
			}
			i += size
		}
	}
	buf.WriteByte('"')
}

func appendJSONValue(buf *bytes.Buffer, v any) {
	switch val := v.(type) {
	case nil:
		buf.WriteString("null")
	case string:
		appendJSONString(buf, val)
	case int:
		buf.WriteString(strconv.Itoa(val))
	case int64:
		buf.WriteString(strconv.FormatInt(val, 10))
	case float64:
		buf.WriteString(strconv.FormatFloat(val, 'f', -1, 64))
	case bool:
		buf.WriteString(strconv.FormatBool(val))
	default:
		appendJSONString(buf, fmt.Sprintf("%v", val))
	}
}
