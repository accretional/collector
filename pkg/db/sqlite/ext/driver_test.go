//go:build cgo

package ext

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func TestDriverOpen(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Verify connection works
	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}
}

func TestDriverOpenWithFilePrefix(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := "file:" + filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database with file: prefix: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}
}

func TestDriverOpenWithQueryParams(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db") + "?_journal_mode=WAL"

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database with query params: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}
}

func TestDriverExec(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create table
	_, err = db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT, value REAL, data BLOB)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert data
	result, err := db.Exec(`INSERT INTO test (name, value, data) VALUES (?, ?, ?)`, "test", 3.14, []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	lastID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("failed to get last insert id: %v", err)
	}
	if lastID != 1 {
		t.Errorf("expected last insert id 1, got %d", lastID)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("failed to get rows affected: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestDriverQuery(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Create and populate table
	_, err = db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT, value REAL, data BLOB)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	_, err = db.Exec(`INSERT INTO test (name, value, data) VALUES (?, ?, ?)`, "row1", 1.1, []byte{1})
	if err != nil {
		t.Fatalf("failed to insert row1: %v", err)
	}
	_, err = db.Exec(`INSERT INTO test (name, value, data) VALUES (?, ?, ?)`, "row2", 2.2, []byte{2, 3})
	if err != nil {
		t.Fatalf("failed to insert row2: %v", err)
	}

	// Query data
	rows, err := db.Query(`SELECT id, name, value, data FROM test ORDER BY id`)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	type row struct {
		id    int64
		name  string
		value float64
		data  []byte
	}
	var results []row

	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.value, &r.data); err != nil {
			t.Fatalf("failed to scan: %v", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	if results[0].name != "row1" || results[0].value != 1.1 {
		t.Errorf("row1 mismatch: got name=%s value=%f", results[0].name, results[0].value)
	}
	if results[1].name != "row2" || results[1].value != 2.2 {
		t.Errorf("row2 mismatch: got name=%s value=%f", results[1].name, results[1].value)
	}
}

func TestDriverQueryWithNulls(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT, value REAL)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert row with NULL values
	_, err = db.Exec(`INSERT INTO test (name, value) VALUES (NULL, NULL)`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	var name sql.NullString
	var value sql.NullFloat64

	err = db.QueryRow(`SELECT name, value FROM test WHERE id = 1`).Scan(&name, &value)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if name.Valid {
		t.Errorf("expected name to be NULL")
	}
	if value.Valid {
		t.Errorf("expected value to be NULL")
	}
}

func TestDriverTransaction(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Test commit
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	_, err = tx.Exec(`INSERT INTO test (name) VALUES (?)`, "committed")
	if err != nil {
		t.Fatalf("failed to insert in tx: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Verify committed data
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM test WHERE name = 'committed'`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 committed row, got %d", count)
	}

	// Test rollback
	tx, err = db.Begin()
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	_, err = tx.Exec(`INSERT INTO test (name) VALUES (?)`, "rolled_back")
	if err != nil {
		t.Fatalf("failed to insert in tx: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("failed to rollback: %v", err)
	}

	// Verify rolled back data
	err = db.QueryRow(`SELECT COUNT(*) FROM test WHERE name = 'rolled_back'`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rolled back rows, got %d", count)
	}
}

func TestDriverBindTypes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE test (
		int_val INTEGER,
		float_val REAL,
		text_val TEXT,
		blob_val BLOB
	)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Test various types
	testCases := []struct {
		name     string
		intVal   int64
		floatVal float64
		textVal  string
		blobVal  []byte
	}{
		{"basic", 42, 3.14159, "hello", []byte{1, 2, 3, 4, 5}},
		{"zeros", 0, 0.0, "", []byte{}},
		{"negative", -100, -1.5, "negative", []byte{255}},
		{"large", 9223372036854775807, 1.7976931348623157e308, "large string with unicode: \u4e2d\u6587", make([]byte, 1000)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(`INSERT INTO test (int_val, float_val, text_val, blob_val) VALUES (?, ?, ?, ?)`,
				tc.intVal, tc.floatVal, tc.textVal, tc.blobVal)
			if err != nil {
				t.Fatalf("failed to insert: %v", err)
			}

			var intVal int64
			var floatVal float64
			var textVal string
			var blobVal []byte

			err = db.QueryRow(`SELECT int_val, float_val, text_val, blob_val FROM test WHERE int_val = ?`, tc.intVal).
				Scan(&intVal, &floatVal, &textVal, &blobVal)
			if err != nil {
				t.Fatalf("failed to query: %v", err)
			}

			if intVal != tc.intVal {
				t.Errorf("int mismatch: got %d, want %d", intVal, tc.intVal)
			}
			if floatVal != tc.floatVal {
				t.Errorf("float mismatch: got %f, want %f", floatVal, tc.floatVal)
			}
			if textVal != tc.textVal {
				t.Errorf("text mismatch: got %q, want %q", textVal, tc.textVal)
			}
			if len(blobVal) != len(tc.blobVal) {
				t.Errorf("blob length mismatch: got %d, want %d", len(blobVal), len(tc.blobVal))
			}
		})
	}
}

func TestDriverConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Enable WAL mode for better concurrency
	_, err = db.Exec(`PRAGMA journal_mode=WAL`)
	if err != nil {
		t.Fatalf("failed to set WAL mode: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, value INTEGER)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert initial data
	for i := 0; i < 100; i++ {
		_, err = db.Exec(`INSERT INTO test (value) VALUES (?)`, i)
		if err != nil {
			t.Fatalf("failed to insert: %v", err)
		}
	}

	// Concurrent reads
	var wg sync.WaitGroup
	errChan := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				var count int
				err := db.QueryRow(`SELECT COUNT(*) FROM test`).Scan(&count)
				if err != nil {
					errChan <- err
					return
				}
				if count < 100 {
					errChan <- err
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			t.Errorf("concurrent read error: %v", err)
		}
	}
}

func TestDBWrapper(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Test Open without extension (will fail as no valid extension path)
	_, err := Open(ctx, dbPath, "/nonexistent/extension.so", "init")
	if err == nil {
		t.Error("expected error opening with nonexistent extension")
	}
}

func TestDBWrapperOperations(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// First create the database without extension
	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	db.Close()

	// Now use the wrapper directly (skipping extension loading for this test)
	// We'll test QueryContext and ExecContext through sql.DB
	db2, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	defer db2.Close()

	conn, err := db2.Conn(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	defer conn.Close()

	// Test ExecContext
	_, err = conn.ExecContext(ctx, `INSERT INTO test (name) VALUES (?)`, "test1")
	if err != nil {
		t.Fatalf("ExecContext failed: %v", err)
	}

	// Test QueryContext
	rows, err := conn.QueryContext(ctx, `SELECT id, name FROM test`)
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		count++
	}

	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestConnClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Get a connection
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Close connection
	if err := conn.Close(); err != nil {
		t.Errorf("failed to close connection: %v", err)
	}

	// Close database
	if err := db.Close(); err != nil {
		t.Errorf("failed to close database: %v", err)
	}

	// Verify database is closed by trying to ping
	if err := db.Ping(); err == nil {
		t.Error("expected error after closing database")
	}
}

func TestStmtNumInput(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE test (a INTEGER, b TEXT, c REAL)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Prepare statement with parameters
	stmt, err := db.Prepare(`INSERT INTO test (a, b, c) VALUES (?, ?, ?)`)
	if err != nil {
		t.Fatalf("failed to prepare: %v", err)
	}
	defer stmt.Close()

	// Execute with correct number of args
	_, err = stmt.Exec(1, "test", 1.5)
	if err != nil {
		t.Errorf("failed to exec with correct args: %v", err)
	}
}

func TestEmptyBlobHandling(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE test (data BLOB)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert empty blob
	_, err = db.Exec(`INSERT INTO test (data) VALUES (?)`, []byte{})
	if err != nil {
		t.Fatalf("failed to insert empty blob: %v", err)
	}

	// Read it back
	var data []byte
	err = db.QueryRow(`SELECT data FROM test`).Scan(&data)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if data == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(data) != 0 {
		t.Errorf("expected empty slice, got %d bytes", len(data))
	}
}

func TestRowsColumns(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE test (alpha INTEGER, beta TEXT, gamma REAL)`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	_, err = db.Exec(`INSERT INTO test VALUES (1, 'test', 1.5)`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	rows, err := db.Query(`SELECT alpha, beta, gamma FROM test`)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("failed to get columns: %v", err)
	}

	expected := []string{"alpha", "beta", "gamma"}
	if len(cols) != len(expected) {
		t.Fatalf("expected %d columns, got %d", len(expected), len(cols))
	}

	for i, col := range cols {
		if col != expected[i] {
			t.Errorf("column %d: got %q, want %q", i, col, expected[i])
		}
	}
}

// BenchmarkInsert measures insert performance
func BenchmarkInsert(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		b.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	db.Exec(`PRAGMA journal_mode=WAL`)
	db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT, value REAL)`)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := db.Exec(`INSERT INTO test (name, value) VALUES (?, ?)`, "test", float64(i))
		if err != nil {
			b.Fatalf("insert failed: %v", err)
		}
	}
}

// BenchmarkQuery measures query performance
func BenchmarkQuery(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")

	db, err := sql.Open(driverName, dbPath)
	if err != nil {
		b.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	db.Exec(`PRAGMA journal_mode=WAL`)
	db.Exec(`CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT, value REAL)`)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		db.Exec(`INSERT INTO test (name, value) VALUES (?, ?)`, "test", float64(i))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rows, err := db.Query(`SELECT id, name, value FROM test WHERE id = ?`, i%1000+1)
		if err != nil {
			b.Fatalf("query failed: %v", err)
		}
		for rows.Next() {
			var id int64
			var name string
			var value float64
			rows.Scan(&id, &name, &value)
		}
		rows.Close()
	}
}
