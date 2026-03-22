package sqlite

/*
#cgo CFLAGS: -DSQLITE_THREADSAFE=2
#cgo CFLAGS: -DSQLITE_ENABLE_FTS5
#cgo CFLAGS: -DSQLITE_ENABLE_JSON1
#cgo CFLAGS: -DSQLITE_ENABLE_RTREE
#cgo CFLAGS: -DSQLITE_DEFAULT_WAL_SYNCHRONOUS=1
#cgo CFLAGS: -DSQLITE_DEFAULT_JOURNAL_MODE_WAL=1
#cgo CFLAGS: -DSQLITE_DQS=0
#cgo CFLAGS: -DSQLITE_LIKE_DOESNT_MATCH_BLOBS
#cgo CFLAGS: -DSQLITE_USE_ALLOCA
#cgo darwin LDFLAGS: -lm
#cgo linux LDFLAGS: -lm -ldl -lpthread

#include "cgo.h"
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/stanza-go/framework/pkg/log"
)

// Column types returned by SQLite.
const (
	TypeInteger = C.SQLITE_INTEGER
	TypeFloat   = C.SQLITE_FLOAT
	TypeText    = C.SQLITE3_TEXT
	TypeBlob    = C.SQLITE_BLOB
	TypeNull    = C.SQLITE_NULL
)

// Result codes from SQLite.
const (
	resultOK   = C.SQLITE_OK
	resultRow  = C.SQLITE_ROW
	resultDone = C.SQLITE_DONE
)

// Open flags.
const (
	openReadWrite = C.SQLITE_OPEN_READWRITE
	openCreate    = C.SQLITE_OPEN_CREATE
	openNoMutex   = C.SQLITE_OPEN_NOMUTEX
)

// DB is a SQLite database connection. It is safe for concurrent use;
// all operations are serialized through a mutex. For the target scale
// of hundreds to low thousands of users, a single connection with a
// mutex provides simple, correct concurrency without the complexity
// of a connection pool.
//
// DB integrates with the lifecycle package via Start and Stop methods:
//
//	lc.Append(lifecycle.Hook{
//	    OnStart: db.Start,
//	    OnStop:  db.Stop,
//	})
type DB struct {
	mu             sync.Mutex
	db             *C.sqlite3
	path           string
	opts           []Option
	migrations     []Migration
	lastBackupPath string
	logger         *log.Logger
	slowThreshold  time.Duration
}

// Option configures a DB.
type Option func(*dbConfig)

type dbConfig struct {
	busyTimeout   int // milliseconds
	pragmas       []string
	logger        *log.Logger
	slowThreshold time.Duration
}

// WithBusyTimeout sets the busy timeout in milliseconds. The default
// is 5000 (5 seconds). When multiple operations contend for the
// database, SQLite retries for up to this duration before returning
// SQLITE_BUSY.
func WithBusyTimeout(ms int) Option {
	return func(c *dbConfig) {
		c.busyTimeout = ms
	}
}

// WithPragma adds a PRAGMA statement to execute after opening the
// database. The value should be a full PRAGMA statement without the
// trailing semicolon, e.g., "PRAGMA cache_size = -64000".
func WithPragma(pragma string) Option {
	return func(c *dbConfig) {
		c.pragmas = append(c.pragmas, pragma)
	}
}

// WithLogger sets a logger for query instrumentation. When set, all
// queries are logged at Debug level with their duration. Queries
// exceeding the slow threshold are logged at Warn level. Failed
// queries are logged at Warn level regardless of duration.
func WithLogger(l *log.Logger) Option {
	return func(c *dbConfig) {
		c.logger = l
	}
}

// WithSlowThreshold sets the duration above which queries are logged
// at Warn level instead of Debug. The default is 200ms. Only takes
// effect when a logger is configured via WithLogger.
func WithSlowThreshold(d time.Duration) Option {
	return func(c *dbConfig) {
		c.slowThreshold = d
	}
}

// New creates a new DB for the given path. The database is not opened
// until Start is called. Use ":memory:" for an in-memory database.
func New(path string, opts ...Option) *DB {
	return &DB{
		path: path,
		opts: opts,
	}
}

// Start opens the database connection and configures it with WAL mode,
// busy timeout, and any additional pragmas. The context is accepted for
// lifecycle compatibility but is not currently used for cancellation.
func (db *DB) Start(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.db != nil {
		return fmt.Errorf("sqlite: already open")
	}

	cpath := C.CString(db.path)
	defer C.free(unsafe.Pointer(cpath))

	flags := C.int(openReadWrite | openCreate | openNoMutex)
	rc := C._open(cpath, &db.db, flags)
	if rc != resultOK {
		var msg string
		if db.db != nil {
			msg = C.GoString(C._errmsg(db.db))
			C._close(db.db)
			db.db = nil
		}
		return fmt.Errorf("sqlite: open %s: %s (code %d)", db.path, msg, rc)
	}

	cfg := dbConfig{busyTimeout: 5000}
	for _, opt := range db.opts {
		opt(&cfg)
	}

	db.logger = cfg.logger
	db.slowThreshold = cfg.slowThreshold
	if db.slowThreshold == 0 && db.logger != nil {
		db.slowThreshold = 200 * time.Millisecond
	}

	rc = C._busy_timeout(db.db, C.int(cfg.busyTimeout))
	if rc != resultOK {
		msg := C.GoString(C._errmsg(db.db))
		C._close(db.db)
		db.db = nil
		return fmt.Errorf("sqlite: busy_timeout: %s", msg)
	}

	// Default pragmas for performance and safety.
	defaults := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 268435456",
		"PRAGMA cache_size = -64000",
	}

	for _, pragma := range defaults {
		if err := db.execLocked(pragma); err != nil {
			C._close(db.db)
			db.db = nil
			return fmt.Errorf("sqlite: %s: %w", pragma, err)
		}
	}

	for _, pragma := range cfg.pragmas {
		if err := db.execLocked(pragma); err != nil {
			C._close(db.db)
			db.db = nil
			return fmt.Errorf("sqlite: %s: %w", pragma, err)
		}
	}

	return nil
}

// Stop closes the database connection gracefully. The context is
// accepted for lifecycle compatibility but is not used for cancellation.
func (db *DB) Stop(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.db == nil {
		return nil
	}

	rc := C._close(db.db)
	db.db = nil
	if rc != resultOK {
		return fmt.Errorf("sqlite: close: code %d", rc)
	}
	return nil
}

// Exec executes a SQL statement that does not return rows (INSERT,
// UPDATE, DELETE, CREATE, etc.). Use args to bind parameters using
// positional placeholders (?).
func (db *DB) Exec(sql string, args ...any) (Result, error) {
	start := time.Now()

	db.mu.Lock()
	defer db.mu.Unlock()

	if db.db == nil {
		return Result{}, fmt.Errorf("sqlite: database not open")
	}

	stmt, err := db.prepare(sql)
	if err != nil {
		db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}
	defer C._finalize(stmt)

	if err := db.bind(stmt, args); err != nil {
		db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}

	rc := C._step(stmt)
	if rc != resultDone && rc != resultRow {
		err := fmt.Errorf("sqlite: exec: %s", C.GoString(C._errmsg(db.db)))
		db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}

	result := Result{
		LastInsertID: int64(C._last_insert_rowid(db.db)),
		RowsAffected: int64(C._changes(db.db)),
	}
	db.logQuery(sql, time.Since(start), nil)
	return result, nil
}

// Query executes a SQL statement that returns rows (SELECT). Use args
// to bind parameters using positional placeholders (?). The caller
// must call Rows.Close when done iterating.
func (db *DB) Query(sql string, args ...any) (*Rows, error) {
	start := time.Now()

	db.mu.Lock()
	// mu is held until Rows.Close is called.

	if db.db == nil {
		db.mu.Unlock()
		return nil, fmt.Errorf("sqlite: database not open")
	}

	stmt, err := db.prepare(sql)
	if err != nil {
		db.logQuery(sql, time.Since(start), err)
		db.mu.Unlock()
		return nil, err
	}

	if err := db.bind(stmt, args); err != nil {
		C._finalize(stmt)
		db.logQuery(sql, time.Since(start), err)
		db.mu.Unlock()
		return nil, err
	}

	return &Rows{
		db:    db,
		stmt:  stmt,
		start: start,
		sql:   sql,
	}, nil
}

// QueryRow executes a SQL statement that returns at most one row.
// Use args to bind parameters using positional placeholders (?).
func (db *DB) QueryRow(sql string, args ...any) *Row {
	rows, err := db.Query(sql, args...)
	if err != nil {
		return &Row{err: err}
	}
	return &Row{rows: rows}
}

// Path returns the database file path.
func (db *DB) Path() string {
	return db.path
}

// Backup creates a complete, consistent copy of the database at destPath
// using VACUUM INTO. Unlike a raw file copy, this includes all WAL data
// and produces a compacted, defragmented database file. Safe to call
// while the database is in use. If destPath already exists, it is
// removed before creating the backup.
func (db *DB) Backup(destPath string) error {
	// VACUUM INTO refuses to overwrite — remove any existing file.
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sqlite: backup: remove existing: %w", err)
	}
	_, err := db.Exec("VACUUM INTO ?", destPath)
	if err != nil {
		return fmt.Errorf("sqlite: backup: %w", err)
	}
	return nil
}

// logQuery logs a completed query with its duration. Normal queries
// are logged at Debug level. Slow queries (above the configured
// threshold) and failed queries are logged at Warn level. No-op
// when no logger is configured.
func (db *DB) logQuery(sql string, dur time.Duration, err error) {
	if db.logger == nil {
		return
	}
	fields := []log.Field{
		log.String("sql", sql),
		log.Duration("duration", dur),
	}
	if err != nil {
		fields = append(fields, log.Err(err))
		db.logger.Warn("query failed", fields...)
		return
	}
	if dur >= db.slowThreshold {
		db.logger.Warn("slow query", fields...)
		return
	}
	db.logger.Debug("query", fields...)
}

// prepare compiles a SQL statement. The caller must hold db.mu.
func (db *DB) prepare(sql string) (*C.sqlite3_stmt, error) {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var stmt *C.sqlite3_stmt
	rc := C._prepare(db.db, csql, C.int(len(sql)), &stmt, nil)
	if rc != resultOK {
		return nil, fmt.Errorf("sqlite: prepare: %s", C.GoString(C._errmsg(db.db)))
	}
	if stmt == nil {
		return nil, fmt.Errorf("sqlite: prepare: empty statement")
	}
	return stmt, nil
}

// bind binds args to a prepared statement. The caller must hold db.mu.
func (db *DB) bind(stmt *C.sqlite3_stmt, args []any) error {
	for i, arg := range args {
		col := C.int(i + 1) // SQLite parameters are 1-indexed
		var rc C.int

		switch v := arg.(type) {
		case nil:
			rc = C._bind_null(stmt, col)
		case int:
			rc = C._bind_int64(stmt, col, C.longlong(v))
		case int64:
			rc = C._bind_int64(stmt, col, C.longlong(v))
		case int32:
			rc = C._bind_int64(stmt, col, C.longlong(v))
		case float64:
			rc = C._bind_double(stmt, col, C.double(v))
		case float32:
			rc = C._bind_double(stmt, col, C.double(v))
		case bool:
			if v {
				rc = C._bind_int64(stmt, col, 1)
			} else {
				rc = C._bind_int64(stmt, col, 0)
			}
		case string:
			cs := C.CString(v)
			rc = C._bind_text(stmt, col, cs, C.int(len(v)))
			C.free(unsafe.Pointer(cs))
		case []byte:
			if len(v) == 0 {
				rc = C._bind_blob(stmt, col, nil, 0)
			} else {
				rc = C._bind_blob(stmt, col, unsafe.Pointer(&v[0]), C.int(len(v)))
			}
		default:
			return fmt.Errorf("sqlite: bind: unsupported type %T at index %d", arg, i)
		}

		if rc != resultOK {
			return fmt.Errorf("sqlite: bind %d: %s", i, C.GoString(C._errmsg(db.db)))
		}
	}
	return nil
}

// execLocked executes a simple SQL statement without parameters.
// The caller must hold db.mu.
func (db *DB) execLocked(sql string) error {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var errmsg *C.char
	rc := C._exec(db.db, csql, &errmsg)
	if rc != resultOK {
		msg := C.GoString(errmsg)
		C.free(unsafe.Pointer(errmsg))
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// Result holds the outcome of an Exec call.
type Result struct {
	LastInsertID int64
	RowsAffected int64
}

// Rows iterates over query results. When created outside a transaction,
// it holds the database mutex for the duration of iteration, so callers
// should close rows promptly. When created inside a transaction, the
// transaction owns the mutex.
//
// Fields are ordered to minimize padding: pointers and slices first,
// then the error interface, then bools packed together at the end.
type Rows struct {
	db      *DB
	stmt    *C.sqlite3_stmt
	start   time.Time
	cols    []string
	sql     string
	err     error
	closed  bool
	stepped bool
	hasRow  bool
	tx      bool // true when rows belong to a transaction
}

// Next advances to the next row. It returns true if there is a row
// available and false when iteration is complete or an error occurs.
func (r *Rows) Next() bool {
	if r.closed {
		return false
	}
	rc := C._step(r.stmt)
	if rc == resultRow {
		r.stepped = true
		r.hasRow = true
		return true
	}
	r.hasRow = false
	if rc != resultDone {
		r.err = fmt.Errorf("sqlite: step: %s", C.GoString(C._errmsg(r.db.db)))
	}
	return false
}

// Err returns the error, if any, that was encountered during iteration.
// Err should be called after Next returns false to check whether
// iteration stopped due to an error or normal completion.
func (r *Rows) Err() error {
	return r.err
}

// Scan reads the current row's columns into the provided dest pointers.
// Supported dest types: *int, *int64, *float64, *string, *[]byte, *bool, *any.
func (r *Rows) Scan(dest ...any) error {
	if !r.hasRow {
		return fmt.Errorf("sqlite: scan: no row")
	}

	n := int(C._column_count(r.stmt))
	if len(dest) != n {
		return fmt.Errorf("sqlite: scan: expected %d columns, got %d destinations", n, len(dest))
	}

	for i, d := range dest {
		col := C.int(i)
		colType := C._column_type(r.stmt, col)

		switch ptr := d.(type) {
		case *int:
			*ptr = int(C._column_int64(r.stmt, col))
		case *int64:
			*ptr = int64(C._column_int64(r.stmt, col))
		case *int32:
			*ptr = int32(C._column_int64(r.stmt, col))
		case *float64:
			*ptr = float64(C._column_double(r.stmt, col))
		case *float32:
			*ptr = float32(C._column_double(r.stmt, col))
		case *string:
			*ptr = C.GoString(C._column_text(r.stmt, col))
		case *[]byte:
			n := C._column_bytes(r.stmt, col)
			if n == 0 || colType == TypeNull {
				*ptr = nil
			} else {
				src := C._column_blob(r.stmt, col)
				buf := make([]byte, int(n))
				copy(buf, unsafe.Slice((*byte)(src), int(n)))
				*ptr = buf
			}
		case *bool:
			*ptr = int64(C._column_int64(r.stmt, col)) != 0
		case *any:
			switch colType {
			case TypeInteger:
				*ptr = int64(C._column_int64(r.stmt, col))
			case TypeFloat:
				*ptr = float64(C._column_double(r.stmt, col))
			case TypeText:
				*ptr = C.GoString(C._column_text(r.stmt, col))
			case TypeBlob:
				n := C._column_bytes(r.stmt, col)
				if n == 0 {
					*ptr = []byte{}
				} else {
					src := C._column_blob(r.stmt, col)
					buf := make([]byte, int(n))
					copy(buf, unsafe.Slice((*byte)(src), int(n)))
					*ptr = buf
				}
			case TypeNull:
				*ptr = nil
			}
		default:
			return fmt.Errorf("sqlite: scan: unsupported type %T at index %d", d, i)
		}
	}
	return nil
}

// Columns returns the column names for the result set.
func (r *Rows) Columns() []string {
	if r.cols != nil {
		return r.cols
	}
	n := int(C._column_count(r.stmt))
	r.cols = make([]string, n)
	for i := range n {
		r.cols[i] = C.GoString(C._column_name(r.stmt, C.int(i)))
	}
	return r.cols
}

// Close releases the prepared statement. When rows were created outside
// a transaction, Close also unlocks the database mutex. When rows were
// created inside a transaction, the transaction retains the mutex.
// It is safe to call Close multiple times.
func (r *Rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	C._finalize(r.stmt)
	r.db.logQuery(r.sql, time.Since(r.start), r.err)
	if !r.tx {
		r.db.mu.Unlock()
	}
	return nil
}

// Row is the result of QueryRow. It wraps Rows and automatically
// closes after Scan.
type Row struct {
	rows *Rows
	err  error
}

// Scan reads the first row into dest and closes the underlying Rows.
// If the query returned no rows, Scan returns ErrNoRows.
func (r *Row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer r.rows.Close()

	if !r.rows.Next() {
		return ErrNoRows
	}
	return r.rows.Scan(dest...)
}

// Err returns any error that occurred during query execution.
func (r *Row) Err() error {
	return r.err
}
