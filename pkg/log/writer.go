package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileWriter writes log entries to a file with automatic rotation. Rotation
// occurs when the date changes (UTC) or when the file exceeds maxSize. Old
// rotated files are pruned to keep at most maxFiles. It is safe for concurrent
// use.
//
// File naming:
//
//	stanza.log                  — current log file
//	stanza-2024-01-15.log       — rotated daily file
//	stanza-2024-01-15.1.log     — second rotation on the same day (size-based)
type FileWriter struct {
	mu       sync.Mutex
	dir      string
	file     *os.File
	curDate  string
	size     int64
	maxSize  int64
	maxFiles int
}

// FileOption configures a FileWriter.
type FileOption func(*FileWriter)

// WithMaxSize sets the maximum file size in bytes before rotation.
// Default is 100 MB.
func WithMaxSize(bytes int64) FileOption {
	return func(fw *FileWriter) {
		fw.maxSize = bytes
	}
}

// WithMaxFiles sets the maximum number of rotated log files to keep.
// Default is 7.
func WithMaxFiles(n int) FileOption {
	return func(fw *FileWriter) {
		fw.maxFiles = n
	}
}

// NewFileWriter creates a FileWriter that writes to the given directory.
// The directory is created if it does not exist.
func NewFileWriter(dir string, opts ...FileOption) (*FileWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("log: create directory: %w", err)
	}

	fw := &FileWriter{
		dir:      dir,
		maxSize:  100 * 1024 * 1024,
		maxFiles: 7,
	}
	for _, opt := range opts {
		opt(fw)
	}

	if err := fw.open(); err != nil {
		return nil, err
	}
	return fw, nil
}

// Write implements io.Writer. It checks for rotation conditions before each
// write.
func (fw *FileWriter) Write(p []byte) (int, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	if today != fw.curDate || fw.size >= fw.maxSize {
		if err := fw.rotate(today); err != nil {
			return 0, err
		}
	}

	n, err := fw.file.Write(p)
	fw.size += int64(n)
	return n, err
}

// Close closes the underlying file.
func (fw *FileWriter) Close() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.file != nil {
		return fw.file.Close()
	}
	return nil
}

func (fw *FileWriter) open() error {
	path := filepath.Join(fw.dir, "stanza.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("log: open file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("log: stat file: %w", err)
	}

	fw.file = f
	fw.size = info.Size()
	fw.curDate = time.Now().UTC().Format("2006-01-02")
	return nil
}

func (fw *FileWriter) rotate(today string) error {
	current := filepath.Join(fw.dir, "stanza.log")
	if _, err := os.Stat(current); err == nil && fw.size > 0 {
		dated := filepath.Join(fw.dir, fmt.Sprintf("stanza-%s.log", fw.curDate))
		if _, err := os.Stat(dated); err == nil {
			for i := 1; ; i++ {
				dated = filepath.Join(fw.dir, fmt.Sprintf("stanza-%s.%d.log", fw.curDate, i))
				if _, err := os.Stat(dated); err != nil {
					break
				}
			}
		}
		_ = os.Rename(current, dated)
	}

	fw.prune()

	// Open the new file before closing the old one so fw.file is never nil
	// on error.
	f, err := os.OpenFile(current, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("log: open new file: %w", err)
	}

	if fw.file != nil {
		_ = fw.file.Close()
	}

	fw.file = f
	fw.size = 0
	fw.curDate = today
	return nil
}

func (fw *FileWriter) prune() {
	entries, err := os.ReadDir(fw.dir)
	if err != nil {
		return
	}

	var rotated []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "stanza.log" {
			continue
		}
		if strings.HasPrefix(name, "stanza-") && strings.HasSuffix(name, ".log") {
			rotated = append(rotated, name)
		}
	}

	if len(rotated) <= fw.maxFiles {
		return
	}

	sort.Strings(rotated)
	for _, name := range rotated[:len(rotated)-fw.maxFiles] {
		_ = os.Remove(filepath.Join(fw.dir, name))
	}
}
