//go:build cgo

// Package ext provides a CGo-based SQLite driver that supports loading
// native extensions. This is an internal package used by the sqlite package
// when extension support is needed (e.g., for vector search via sqlite-vec).
//
// This driver is minimal by design - it implements only what's needed for
// extension loading and basic query execution. For regular database operations,
// the sqlite package uses modernc.org/sqlite (pure Go).
//
// This package is internal to pkg/db/sqlite and should not be imported directly.
package ext

/*
#cgo CFLAGS: -DSQLITE_ENABLE_LOAD_EXTENSION=1
#cgo LDFLAGS: -lsqlite3

#include <sqlite3.h>
#include <stdlib.h>
#include <string.h>

// load_extension enables extension loading and loads the specified extension.
// It disables extension loading after completion for security.
static int load_extension(sqlite3 *db, const char *path, const char *entry, char **errmsg) {
    int rc = sqlite3_enable_load_extension(db, 1);
    if (rc != SQLITE_OK) {
        *errmsg = (char*)sqlite3_errmsg(db);
        return rc;
    }
    rc = sqlite3_load_extension(db, path, entry, errmsg);
    sqlite3_enable_load_extension(db, 0);
    return rc;
}

static int bind_text_copy(sqlite3_stmt *stmt, int idx, const char *text, int len) {
    char *copy = (char*)malloc(len);
    if (copy == NULL && len > 0) return SQLITE_NOMEM;
    if (len > 0) memcpy(copy, text, len);
    return sqlite3_bind_text(stmt, idx, copy, len, free);
}

static int bind_blob_copy(sqlite3_stmt *stmt, int idx, const void *data, int len) {
    void *copy = malloc(len);
    if (copy == NULL && len > 0) return SQLITE_NOMEM;
    if (len > 0) memcpy(copy, data, len);
    return sqlite3_bind_blob(stmt, idx, copy, len, free);
}

static void cfree(void *p) {
    free(p);
}
*/
import "C"

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"unsafe"
)

const driverName = "sqliteext"

func init() {
	sql.Register(driverName, &Driver{})
}

type Driver struct{}

func (*Driver) Open(dsn string) (driver.Conn, error) {
	if len(dsn) > 5 && dsn[:5] == "file:" {
		dsn = dsn[5:]
	}
	if idx := strings.IndexByte(dsn, '?'); idx != -1 {
		dsn = dsn[:idx]
	}

	cDsn := C.CString(dsn)
	defer C.cfree(unsafe.Pointer(cDsn))

	var db *C.sqlite3
	rc := C.sqlite3_open_v2(cDsn, &db,
		C.SQLITE_OPEN_READWRITE|C.SQLITE_OPEN_CREATE|C.SQLITE_OPEN_URI,
		nil)
	if rc != C.SQLITE_OK {
		if db != nil {
			C.sqlite3_close(db)
		}
		return nil, fmt.Errorf("failed to open database: %s", C.GoString(C.sqlite3_errmsg(db)))
	}

	return &Conn{db: db}, nil
}

type Conn struct {
	db *C.sqlite3
	mu sync.Mutex
}

func (c *Conn) LoadExtension(path, entryPoint string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cPath := C.CString(path)
	defer C.cfree(unsafe.Pointer(cPath))

	var cEntry *C.char
	if entryPoint != "" {
		cEntry = C.CString(entryPoint)
		defer C.cfree(unsafe.Pointer(cEntry))
	}

	var errMsg *C.char
	rc := C.load_extension(c.db, cPath, cEntry, &errMsg)
	if rc != C.SQLITE_OK {
		msg := "unknown error"
		if errMsg != nil {
			msg = C.GoString(errMsg)
			C.sqlite3_free(unsafe.Pointer(errMsg))
		}
		return fmt.Errorf("failed to load extension %s: %s", path, msg)
	}
	return nil
}

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cQuery := C.CString(query)
	defer C.cfree(unsafe.Pointer(cQuery))

	var stmt *C.sqlite3_stmt
	rc := C.sqlite3_prepare_v2(c.db, cQuery, -1, &stmt, nil)
	if rc != C.SQLITE_OK {
		return nil, fmt.Errorf("prepare: %s", C.GoString(C.sqlite3_errmsg(c.db)))
	}

	return &Stmt{conn: c, stmt: stmt}, nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.db != nil {
		C.sqlite3_close(c.db)
		c.db = nil
	}
	return nil
}

func (c *Conn) Begin() (driver.Tx, error) {
	if _, err := c.execDirect("BEGIN"); err != nil {
		return nil, err
	}
	return &Tx{conn: c}, nil
}

func (c *Conn) execDirect(query string) (sql.Result, error) {
	cQuery := C.CString(query)
	defer C.cfree(unsafe.Pointer(cQuery))

	var errMsg *C.char
	rc := C.sqlite3_exec(c.db, cQuery, nil, nil, &errMsg)
	if rc != C.SQLITE_OK {
		msg := C.GoString(errMsg)
		C.sqlite3_free(unsafe.Pointer(errMsg))
		return nil, fmt.Errorf("exec: %s", msg)
	}
	return &Result{
		lastID:   int64(C.sqlite3_last_insert_rowid(c.db)),
		affected: int64(C.sqlite3_changes(c.db)),
	}, nil
}

type Tx struct {
	conn *Conn
}

func (tx *Tx) Commit() error {
	_, err := tx.conn.execDirect("COMMIT")
	return err
}

func (tx *Tx) Rollback() error {
	_, err := tx.conn.execDirect("ROLLBACK")
	return err
}

type Stmt struct {
	conn *Conn
	stmt *C.sqlite3_stmt
}

func (s *Stmt) Close() error {
	if s.stmt != nil {
		C.sqlite3_finalize(s.stmt)
		s.stmt = nil
	}
	return nil
}

func (s *Stmt) NumInput() int {
	return int(C.sqlite3_bind_parameter_count(s.stmt))
}

func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	s.conn.mu.Lock()
	defer s.conn.mu.Unlock()

	if err := s.bindArgs(args); err != nil {
		return nil, err
	}

	rc := C.sqlite3_step(s.stmt)
	C.sqlite3_reset(s.stmt)

	if rc != C.SQLITE_DONE && rc != C.SQLITE_ROW {
		return nil, fmt.Errorf("exec: %s", C.GoString(C.sqlite3_errmsg(s.conn.db)))
	}

	return &Result{
		lastID:   int64(C.sqlite3_last_insert_rowid(s.conn.db)),
		affected: int64(C.sqlite3_changes(s.conn.db)),
	}, nil
}

func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	s.conn.mu.Lock()
	defer s.conn.mu.Unlock()

	if err := s.bindArgs(args); err != nil {
		return nil, err
	}

	colCount := int(C.sqlite3_column_count(s.stmt))
	cols := make([]string, colCount)
	for i := 0; i < colCount; i++ {
		cols[i] = C.GoString(C.sqlite3_column_name(s.stmt, C.int(i)))
	}

	return &Rows{stmt: s, cols: cols}, nil
}

func (s *Stmt) bindArgs(args []driver.Value) error {
	C.sqlite3_reset(s.stmt)
	C.sqlite3_clear_bindings(s.stmt)

	for i, arg := range args {
		idx := C.int(i + 1)
		var rc C.int

		switch v := arg.(type) {
		case nil:
			rc = C.sqlite3_bind_null(s.stmt, idx)
		case int64:
			rc = C.sqlite3_bind_int64(s.stmt, idx, C.sqlite3_int64(v))
		case float64:
			rc = C.sqlite3_bind_double(s.stmt, idx, C.double(v))
		case string:
			cStr := C.CString(v)
			rc = C.bind_text_copy(s.stmt, idx, cStr, C.int(len(v)))
			C.cfree(unsafe.Pointer(cStr))
		case []byte:
			if len(v) == 0 {
				rc = C.sqlite3_bind_zeroblob(s.stmt, idx, 0)
			} else {
				rc = C.bind_blob_copy(s.stmt, idx, unsafe.Pointer(&v[0]), C.int(len(v)))
			}
		default:
			return fmt.Errorf("unsupported type: %T", arg)
		}

		if rc != C.SQLITE_OK {
			return fmt.Errorf("bind param %d: %s", i, C.GoString(C.sqlite3_errmsg(s.conn.db)))
		}
	}
	return nil
}

type Rows struct {
	stmt *Stmt
	cols []string
}

func (r *Rows) Columns() []string {
	return r.cols
}

func (r *Rows) Close() error {
	C.sqlite3_reset(r.stmt.stmt)
	return nil
}

func (r *Rows) Next(dest []driver.Value) error {
	rc := C.sqlite3_step(r.stmt.stmt)
	if rc == C.SQLITE_DONE {
		return io.EOF
	}
	if rc != C.SQLITE_ROW {
		return fmt.Errorf("step: %s", C.GoString(C.sqlite3_errmsg(r.stmt.conn.db)))
	}

	for i := range dest {
		colType := C.sqlite3_column_type(r.stmt.stmt, C.int(i))
		switch colType {
		case C.SQLITE_NULL:
			dest[i] = nil
		case C.SQLITE_INTEGER:
			dest[i] = int64(C.sqlite3_column_int64(r.stmt.stmt, C.int(i)))
		case C.SQLITE_FLOAT:
			dest[i] = float64(C.sqlite3_column_double(r.stmt.stmt, C.int(i)))
		case C.SQLITE_TEXT:
			dest[i] = C.GoString((*C.char)(unsafe.Pointer(C.sqlite3_column_text(r.stmt.stmt, C.int(i)))))
		case C.SQLITE_BLOB:
			n := C.sqlite3_column_bytes(r.stmt.stmt, C.int(i))
			if n > 0 {
				blob := C.sqlite3_column_blob(r.stmt.stmt, C.int(i))
				dest[i] = C.GoBytes(blob, n)
			} else {
				dest[i] = []byte{}
			}
		}
	}
	return nil
}

type Result struct {
	lastID   int64
	affected int64
}

func (r *Result) LastInsertId() (int64, error) {
	return r.lastID, nil
}

func (r *Result) RowsAffected() (int64, error) {
	return r.affected, nil
}

type DB struct {
	db   *sql.DB
	conn *sql.Conn
}

func Open(ctx context.Context, dbPath, extensionPath, entryPoint string) (*DB, error) {
	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("get connection: %w", err)
	}

	err = conn.Raw(func(driverConn any) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return fmt.Errorf("unexpected connection type: %T", driverConn)
		}
		return c.LoadExtension(extensionPath, entryPoint)
	})
	if err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("load extension: %w", err)
	}

	// Set WAL mode for better concurrency
	if _, err := conn.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	return &DB{db: db, conn: conn}, nil
}

func (d *DB) Close() error {
	if d.conn != nil {
		d.conn.Close()
	}
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *DB) Conn() *sql.Conn {
	return d.conn
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.conn.QueryContext(ctx, query, args...)
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.conn.ExecContext(ctx, query, args...)
}
