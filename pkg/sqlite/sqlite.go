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
	"sync/atomic"
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

// DBStats holds connection pool and query statistics. Use DB.Stats to
// obtain a snapshot.
type DBStats struct {
	// ReadPoolSize is the configured number of read connections.
	ReadPoolSize int `json:"read_pool_size"`
	// ReadPoolAvailable is the number of idle connections in the pool.
	ReadPoolAvailable int `json:"read_pool_available"`
	// ReadPoolInUse is the number of connections currently checked out.
	ReadPoolInUse int `json:"read_pool_in_use"`
	// TotalReads is the total number of read queries executed.
	TotalReads int64 `json:"total_reads"`
	// TotalWrites is the total number of write operations executed.
	TotalWrites int64 `json:"total_writes"`
	// PoolWaits is the number of times a read query had to wait for
	// a free connection because all pool connections were in use.
	PoolWaits int64 `json:"pool_waits"`
	// PoolWaitTime is the cumulative time spent waiting for a free
	// connection from the read pool.
	PoolWaitTime time.Duration `json:"pool_wait_time"`
	// FileSize is the size of the main database file in bytes. Zero
	// for in-memory databases or if the file cannot be stat'd.
	FileSize int64 `json:"file_size"`
	// WALSize is the size of the WAL (write-ahead log) file in bytes.
	// Zero when there is no WAL file or for in-memory databases.
	WALSize int64 `json:"wal_size"`
}

// DB is a SQLite database connection with read/write separation. Write
// operations (Exec, transactions) use a dedicated write connection,
// while read operations (Query, QueryRow) use a pool of read
// connections. This lets multiple reads proceed concurrently with each
// other and with writes in WAL mode — HTTP reads are not blocked by
// cron jobs or queue workers writing to the database, and concurrent
// HTTP requests can read simultaneously.
//
// For in-memory databases (:memory:), a single connection is used
// because each :memory: connection creates a separate database.
//
// DB integrates with the lifecycle package via Start and Stop methods:
//
//	lc.Append(lifecycle.Hook{
//	    OnStart: db.Start,
//	    OnStop:  db.Stop,
//	})
type DB struct {
	mu             sync.Mutex      // protects write connection
	db             *C.sqlite3      // write connection
	readPool       chan *C.sqlite3  // pool of read connections (nil for :memory:)
	poolDone       chan struct{}    // closed on Stop to unblock pool waiters
	poolWg         sync.WaitGroup  // tracks connections checked out from pool
	poolStopping   atomic.Bool     // true after poolDone is closed
	totalReads     atomic.Int64    // total read queries executed
	totalWrites    atomic.Int64    // total write operations executed
	poolWaits      atomic.Int64    // times a read query waited for a free connection
	poolWaitNs     atomic.Int64    // cumulative nanoseconds spent waiting for pool
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
	readPoolSize  int // number of read connections
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

// WithReadPoolSize sets the number of read connections in the pool.
// The default is 4. More connections allow more concurrent reads,
// which helps when multiple HTTP requests query the database
// simultaneously. Only applies to file-backed databases — in-memory
// databases always use a single connection.
func WithReadPoolSize(n int) Option {
	return func(c *dbConfig) {
		c.readPoolSize = n
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

// Start opens the database connections and configures them with WAL
// mode, busy timeout, and any additional pragmas. One write connection
// and a pool of read connections are opened. The pool allows multiple
// reads to proceed concurrently with each other and with writes in
// WAL mode. For in-memory databases, only a single connection is used
// because each :memory: open creates a separate database.
func (db *DB) Start(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.db != nil {
		return fmt.Errorf("sqlite: already open")
	}

	cfg := dbConfig{busyTimeout: 5000, readPoolSize: 4}
	for _, opt := range db.opts {
		opt(&cfg)
	}
	if cfg.readPoolSize < 1 {
		cfg.readPoolSize = 1
	}

	db.logger = cfg.logger
	db.slowThreshold = cfg.slowThreshold
	if db.slowThreshold == 0 && db.logger != nil {
		db.slowThreshold = 200 * time.Millisecond
	}

	// Open the write connection.
	writeConn, err := db.openConn(cfg)
	if err != nil {
		return err
	}
	db.db = writeConn

	// Open a pool of read connections for non-memory databases.
	// In-memory databases create a new database per connection, so
	// read and write must share the same connection.
	if db.path != ":memory:" && db.path != "" {
		pool := make(chan *C.sqlite3, cfg.readPoolSize)
		for range cfg.readPoolSize {
			rc, err := db.openConn(cfg)
			if err != nil {
				// Close any already-opened read connections.
				close(pool)
				for conn := range pool {
					C._close(conn)
				}
				C._close(db.db)
				db.db = nil
				return fmt.Errorf("sqlite: open read connection: %w", err)
			}
			pool <- rc
		}
		db.readPool = pool
		db.poolDone = make(chan struct{})
	}

	return nil
}

// openConn opens a single SQLite connection and configures it with
// busy timeout, default pragmas, and any user-supplied pragmas. The
// caller must hold db.mu.
func (db *DB) openConn(cfg dbConfig) (*C.sqlite3, error) {
	cpath := C.CString(db.path)
	defer C.free(unsafe.Pointer(cpath))

	var conn *C.sqlite3
	flags := C.int(openReadWrite | openCreate | openNoMutex)
	rc := C._open(cpath, &conn, flags)
	if rc != resultOK {
		var msg string
		if conn != nil {
			msg = C.GoString(C._errmsg(conn))
			C._close(conn)
		}
		return nil, fmt.Errorf("sqlite: open %s: %s (code %d)", db.path, msg, rc)
	}

	rc = C._busy_timeout(conn, C.int(cfg.busyTimeout))
	if rc != resultOK {
		msg := C.GoString(C._errmsg(conn))
		C._close(conn)
		return nil, fmt.Errorf("sqlite: busy_timeout: %s", msg)
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
		if err := db.execOnConn(conn, pragma); err != nil {
			C._close(conn)
			return nil, fmt.Errorf("sqlite: %s: %w", pragma, err)
		}
	}

	for _, pragma := range cfg.pragmas {
		if err := db.execOnConn(conn, pragma); err != nil {
			C._close(conn)
			return nil, fmt.Errorf("sqlite: %s: %w", pragma, err)
		}
	}

	return conn, nil
}

// Stop closes all database connections gracefully. It signals pool
// operations to stop, waits for all checked-out connections to be
// returned (or closed directly), drains the pool channel, then closes
// the write connection. The context is accepted for lifecycle
// compatibility but is not used for cancellation.
func (db *DB) Stop(ctx context.Context) error {
	if db.readPool != nil {
		// Signal all pool operations to stop. This unblocks any
		// goroutines waiting for a connection in queryPool.
		close(db.poolDone)

		// Mark the pool as stopping so returnToPool closes
		// connections directly instead of sending to the channel.
		db.poolStopping.Store(true)

		// Wait for all checked-out connections to be returned or
		// closed directly by returnToPool.
		db.poolWg.Wait()

		// All checked-out connections are accounted for. Drain any
		// connections remaining in the channel (those that were idle
		// or returned before poolStopping was set).
		close(db.readPool)
		for conn := range db.readPool {
			rc := C._close(conn)
			if rc != resultOK {
				return fmt.Errorf("sqlite: close read connection: code %d", rc)
			}
		}
		db.readPool = nil
	}

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
// UPDATE, DELETE, CREATE, etc.). Uses the write connection. Use args
// to bind parameters using positional placeholders (?).
func (db *DB) Exec(sql string, args ...any) (Result, error) {
	start := time.Now()

	db.mu.Lock()
	defer db.mu.Unlock()

	if db.db == nil {
		return Result{}, fmt.Errorf("sqlite: database not open")
	}

	stmt, err := db.prepare(db.db, sql)
	if err != nil {
		db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}
	defer C._finalize(stmt)

	if err := db.bind(db.db, stmt, args); err != nil {
		db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}

	rc := C._step(stmt)
	if rc != resultDone && rc != resultRow {
		err := dbError(db.db, "sqlite: exec")
		db.logQuery(sql, time.Since(start), err)
		return Result{}, err
	}

	result := Result{
		LastInsertID: int64(C._last_insert_rowid(db.db)),
		RowsAffected: int64(C._changes(db.db)),
	}
	db.totalWrites.Add(1)
	db.logQuery(sql, time.Since(start), nil)
	return result, nil
}

// Query executes a SQL statement that returns rows (SELECT). Uses a
// connection from the read pool when available (file-backed
// databases), falling back to the write connection for in-memory
// databases. Use args to bind parameters using positional placeholders
// (?). The caller must call Rows.Close when done iterating — this
// returns the connection to the pool.
func (db *DB) Query(sql string, args ...any) (*Rows, error) {
	// Use a read pool connection if available, otherwise fall back
	// to the write connection with its mutex.
	if db.readPool != nil {
		return db.queryPool(sql, args)
	}
	return db.queryMu(&db.mu, db.db, sql, args)
}

// queryPool takes a connection from the read pool, executes the query,
// and returns Rows that will return the connection on Close. The
// WaitGroup is incremented before waiting so that Stop can wait for
// all checked-out connections to be returned. If the database is
// shutting down (poolDone closed), the select unblocks immediately
// and returns an error.
func (db *DB) queryPool(sql string, args []any) (*Rows, error) {
	start := time.Now()

	// Increment before select so Stop's poolWg.Wait cannot return
	// while a goroutine is between receiving a connection and
	// tracking it.
	db.poolWg.Add(1)

	// Try non-blocking first to detect pool exhaustion.
	var conn *C.sqlite3
	select {
	case conn = <-db.readPool:
	default:
		// All connections busy — record a pool wait.
		db.poolWaits.Add(1)
		waitStart := time.Now()
		select {
		case conn = <-db.readPool:
		case <-db.poolDone:
			db.poolWg.Done()
			return nil, fmt.Errorf("sqlite: database is shutting down")
		}
		db.poolWaitNs.Add(int64(time.Since(waitStart)))
	}

	stmt, err := db.prepare(conn, sql)
	if err != nil {
		db.logQuery(sql, time.Since(start), err)
		db.returnToPool(conn)
		return nil, err
	}

	if err := db.bind(conn, stmt, args); err != nil {
		C._finalize(stmt)
		db.logQuery(sql, time.Since(start), err)
		db.returnToPool(conn)
		return nil, err
	}

	db.totalReads.Add(1)
	return &Rows{
		db:    db,
		conn:  conn,
		pool:  db.readPool,
		stmt:  stmt,
		start: start,
		sql:   sql,
	}, nil
}

// returnToPool returns a read connection to the pool, or closes it
// directly if the database is shutting down. Must be called exactly
// once per connection checked out from the pool.
func (db *DB) returnToPool(conn *C.sqlite3) {
	if db.poolStopping.Load() {
		// Pool is draining — close the connection directly instead
		// of sending to a potentially closed channel.
		C._close(conn)
	} else {
		select {
		case db.readPool <- conn:
		case <-db.poolDone:
			C._close(conn)
		}
	}
	db.poolWg.Done()
}

// queryMu executes a query on the given connection, holding mu for the
// lifetime of the returned Rows.
func (db *DB) queryMu(mu *sync.Mutex, conn *C.sqlite3, sql string, args []any) (*Rows, error) {
	start := time.Now()

	mu.Lock()

	if conn == nil {
		mu.Unlock()
		return nil, fmt.Errorf("sqlite: database not open")
	}

	stmt, err := db.prepare(conn, sql)
	if err != nil {
		db.logQuery(sql, time.Since(start), err)
		mu.Unlock()
		return nil, err
	}

	if err := db.bind(conn, stmt, args); err != nil {
		C._finalize(stmt)
		db.logQuery(sql, time.Since(start), err)
		mu.Unlock()
		return nil, err
	}

	db.totalReads.Add(1)
	return &Rows{
		db:    db,
		conn:  conn,
		mu:    mu,
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

// Count executes a count query derived from the given SelectBuilder and
// returns the number of matching rows. It combines CountFrom, Build, and
// QueryRow into a single call — the common pattern for paginated list
// endpoints.
func (db *DB) Count(sb *SelectBuilder) (int, error) {
	sql, args := CountFrom(sb).Build()
	var n int
	err := db.QueryRow(sql, args...).Scan(&n)
	return n, err
}

// Path returns the database file path.
func (db *DB) Path() string {
	return db.path
}

// Stats returns a snapshot of connection pool and query statistics.
// The counters are cumulative since the database was opened. Pool
// availability is a point-in-time snapshot and may change immediately
// after the call returns.
func (db *DB) Stats() DBStats {
	s := DBStats{
		TotalReads:   db.totalReads.Load(),
		TotalWrites:  db.totalWrites.Load(),
		PoolWaits:    db.poolWaits.Load(),
		PoolWaitTime: time.Duration(db.poolWaitNs.Load()),
	}
	if db.readPool != nil {
		s.ReadPoolSize = cap(db.readPool)
		s.ReadPoolAvailable = len(db.readPool)
		s.ReadPoolInUse = s.ReadPoolSize - s.ReadPoolAvailable
	}
	if db.path != "" && db.path != ":memory:" {
		if info, err := os.Stat(db.path); err == nil {
			s.FileSize = info.Size()
		}
		if info, err := os.Stat(db.path + "-wal"); err == nil {
			s.WALSize = info.Size()
		}
	}
	return s
}

// Now returns the current UTC time formatted as an RFC 3339 string.
// Use this when storing timestamps in SQLite columns — it produces
// the canonical format used throughout the application for created_at,
// updated_at, deleted_at, and similar fields.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Optimize runs PRAGMA optimize, which analyzes tables that the query
// planner has identified as needing updated statistics. Call this before
// closing a long-running database connection — it keeps the query
// planner accurate with minimal overhead (typically a few milliseconds).
func (db *DB) Optimize() error {
	_, err := db.Exec("PRAGMA optimize")
	return err
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

// dbError creates an *Error from the current error state of conn.
// The prefix is prepended to the SQLite error message (e.g.,
// "sqlite: exec" produces "sqlite: exec: UNIQUE constraint failed").
func dbError(conn *C.sqlite3, prefix string) *Error {
	code := int(C._errcode(conn))
	extCode := int(C._extended_errcode(conn))
	msg := C.GoString(C._errmsg(conn))
	if prefix != "" {
		msg = prefix + ": " + msg
	}
	return &Error{Code: code, ExtendedCode: extCode, Message: msg}
}

// prepare compiles a SQL statement on the given connection. The caller
// must hold the mutex that protects conn.
func (db *DB) prepare(conn *C.sqlite3, sql string) (*C.sqlite3_stmt, error) {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var stmt *C.sqlite3_stmt
	rc := C._prepare(conn, csql, C.int(len(sql)), &stmt, nil)
	if rc != resultOK {
		return nil, dbError(conn, "sqlite: prepare")
	}
	if stmt == nil {
		return nil, fmt.Errorf("sqlite: prepare: empty statement")
	}
	return stmt, nil
}

// bind binds args to a prepared statement. The caller must hold the
// mutex that protects conn. conn is used only for error messages.
func (db *DB) bind(conn *C.sqlite3, stmt *C.sqlite3_stmt, args []any) error {
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
			return dbError(conn, fmt.Sprintf("sqlite: bind %d", i))
		}
	}
	return nil
}

// execOnConn executes a simple SQL statement without parameters on the
// given connection. The caller must hold the mutex that protects conn.
func (db *DB) execOnConn(conn *C.sqlite3, sql string) error {
	csql := C.CString(sql)
	defer C.free(unsafe.Pointer(csql))

	var errmsg *C.char
	rc := C._exec(conn, csql, &errmsg)
	if rc != resultOK {
		msg := C.GoString(errmsg)
		C.free(unsafe.Pointer(errmsg))
		return &Error{
			Code:         int(C._errcode(conn)),
			ExtendedCode: int(C._extended_errcode(conn)),
			Message:      msg,
		}
	}
	return nil
}

// execLocked executes a simple SQL statement on the write connection.
// The caller must hold db.mu.
func (db *DB) execLocked(sql string) error {
	return db.execOnConn(db.db, sql)
}

// Result holds the outcome of an Exec call.
type Result struct {
	LastInsertID int64
	RowsAffected int64
}

// Rows iterates over query results. When created from the read pool,
// it holds a connection that is returned to the pool on Close. When
// created from the write connection (in-memory databases), it holds a
// mutex that is unlocked on Close. When created inside a transaction,
// both pool and mu are nil — the transaction owns the write mutex.
//
// Fields are ordered to minimize padding: pointers and channels first,
// then the error interface, then bools packed together at the end.
type Rows struct {
	db      *DB
	conn    *C.sqlite3         // connection this query runs on (for error messages)
	pool    chan *C.sqlite3     // read pool to return conn to on Close (nil if not pooled)
	mu      *sync.Mutex        // mutex to unlock on Close (nil if pooled or tx-owned)
	stmt    *C.sqlite3_stmt
	start   time.Time
	cols    []string
	sql     string
	err     error
	closed  bool
	stepped bool
	hasRow  bool
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
		r.err = dbError(r.conn, "sqlite: step")
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

// Close releases the prepared statement. When rows were created from
// the read pool, Close returns the connection via returnToPool (which
// is safe to call during shutdown). When rows were created from the
// write connection (in-memory databases), Close unlocks the held
// mutex. When rows were created inside a transaction, the transaction
// retains the write mutex. It is safe to call Close multiple times.
func (r *Rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	C._finalize(r.stmt)
	r.db.logQuery(r.sql, time.Since(r.start), r.err)
	if r.pool != nil {
		r.db.returnToPool(r.conn)
	} else if r.mu != nil {
		r.mu.Unlock()
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
