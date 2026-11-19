package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

// needSQLite skips a test in -short mode, and where the plugin's native
// module is not built: mygo native build -target host
// ./plugins/sqlite/internal/sqlitecore builds it, with Zig.
func needSQLite(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires the plugin's native module")
	}
	if err := Load(); err != nil && strings.Contains(err.Error(), "mygo native build") {
		t.Skip(err)
	}
}

func testDB(t *testing.T, path string) *DB {
	t.Helper()
	needSQLite(t)
	db, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func execute(t *testing.T, db *DB, sql string, args ...any) Result {
	t.Helper()
	r, err := db.Execute(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func query(t *testing.T, db *DB, sql string, args ...any) Rows {
	t.Helper()
	r, err := db.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestValues(t *testing.T) {
	db := testDB(t, ":memory:")
	values := []any{nil, int64(math.MinInt64), int64(math.MaxInt64), -3.125, "中文🙂\x00text", []byte{0, 1, 255}, []byte{}, "", true}
	rows := query(t, db, "SELECT ? AS same, ? AS same, ?, ?, ?, ?, ?, ?, ?", values...)
	want := append([]any{}, values...)
	want[8] = int64(1)
	if !reflect.DeepEqual(rows.Values, [][]any{want}) {
		t.Fatalf("values = %#v, want %#v", rows.Values, want)
	}
	if rows.Columns[0] != "same" || rows.Columns[1] != "same" {
		t.Fatal(rows.Columns)
	}
	execute(t, db, "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT UNIQUE, bytes BLOB)")
	r := execute(t, db, "INSERT INTO items(name, bytes) VALUES (?, ?)", "it's a ?; DROP TABLE items; --", []byte{1, 2})
	if r.Changes != 1 || r.LastInsertID != 1 {
		t.Fatal(r)
	}
	if n := execute(t, db, "CREATE TABLE other (n)").Changes; n != 0 {
		t.Fatalf("DDL changes = %d", n)
	}
	got := query(t, db, "SELECT name, bytes FROM items").Values[0]
	if got[0] != "it's a ?; DROP TABLE items; --" || !reflect.DeepEqual(got[1], []byte{1, 2}) {
		t.Fatal(got)
	}
	// BLOB storage belongs to Go after SQLite finalizes the statement.
	_ = query(t, db, "SELECT randomblob(65536)")
	if !reflect.DeepEqual(got[1], []byte{1, 2}) {
		t.Fatal("BLOB changed after a later step")
	}
	if rows := query(t, db, "SELECT * FROM items WHERE 0"); len(rows.Values) != 0 || len(rows.Columns) != 3 {
		t.Fatal(rows)
	}
	if r := query(t, db, "INSERT INTO items(name) VALUES ('returning') RETURNING id"); r.Values[0][0] != int64(2) {
		t.Fatal(r)
	}
	if r := query(t, db, "SELECT ?, ?, ?", json.Number("42"), json.Number("0.25"), uint32(7)); !reflect.DeepEqual(r.Values[0], []any{int64(42), 0.25, int64(7)}) {
		t.Fatal(r)
	}
}

func TestSQLiteFeatures(t *testing.T) {
	db := testDB(t, ":memory:")
	if v := query(t, db, "SELECT sqlite_version()").Values[0][0]; v != "3.53.4" {
		t.Fatalf("SQLite = %v", v)
	}
	execute(t, db, "CREATE TABLE parent (id PRIMARY KEY)")
	execute(t, db, "CREATE TABLE child (parent REFERENCES parent(id))")
	_, err := db.Execute(context.Background(), "INSERT INTO child VALUES (1)")
	var sqliteErr *Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code != 19 || sqliteErr.ExtendedCode != 787 {
		t.Fatalf("foreign key: %v", err)
	}
	execute(t, db, "CREATE VIRTUAL TABLE search USING fts5(body)")
	execute(t, db, "INSERT INTO search VALUES ('hello sqlite')")
	if n := query(t, db, "SELECT count(*) FROM search WHERE search MATCH 'sqlite'").Values[0][0]; n != int64(1) {
		t.Fatal(n)
	}
	execute(t, db, "CREATE VIRTUAL TABLE spatial USING rtree(id, x0, x1, y0, y1)")
	if v := query(t, db, `SELECT json_extract('{"a":42}', '$.a')`).Values[0][0]; v != int64(42) {
		t.Fatal(v)
	}
	execute(t, db, "VACUUM")
}

func TestValidationBeforeExecution(t *testing.T) {
	db := testDB(t, ":memory:")
	execute(t, db, "CREATE TABLE items (n UNIQUE)")
	for _, sql := range []string{"INSERT INTO items VALUES (1); INSERT INTO items VALUES (2)", "INSERT INTO items VALUES (3); invalid sql", "INSERT INTO items VALUES (4)\x00;", "", "-- only a comment", "BEGIN", "SAVEPOINT foo", "ATTACH ':memory:' AS other", "VACUUM INTO 'outside.db'"} {
		if _, err := db.Execute(context.Background(), sql); err == nil {
			t.Errorf("accepted %q", sql)
		}
	}
	if n := query(t, db, "SELECT count(*) FROM items").Values[0][0]; n != int64(0) {
		t.Fatalf("partial SQL ran: %v", n)
	}
	execute(t, db, "INSERT INTO items VALUES (?) ; -- trailing comment\n /* another */", 1)
	for _, args := range [][]any{nil, {1, 2}, {math.NaN()}, {math.Inf(1)}, {uint64(math.MaxUint64)}, {json.Number("9223372036854775808")}, {struct{}{}}, {"\xff"}} {
		if _, err := db.Execute(context.Background(), "INSERT INTO items VALUES (?)", args...); err == nil {
			t.Errorf("accepted args %#v", args)
		}
	}
}

func TestTransactions(t *testing.T) {
	db := testDB(t, ":memory:")
	execute(t, db, "CREATE TABLE items (n UNIQUE)")
	results, err := db.Transaction(context.Background(), []Statement{{"INSERT INTO items VALUES (?)", []any{1}}, {"INSERT INTO items VALUES (?)", []any{2}}})
	if err != nil || len(results) != 2 || results[0].Changes != 1 || results[1].Changes != 1 {
		t.Fatalf("%v, %v", results, err)
	}
	for _, second := range []string{"INSERT INTO items VALUES (2)", "COMMIT", "SAVEPOINT escape", "not sql"} {
		results, err := db.Transaction(context.Background(), []Statement{{SQL: "INSERT INTO items VALUES (3)"}, {SQL: second}})
		if err == nil || results != nil {
			t.Fatalf("batch succeeded: %q: %v %v", second, results, err)
		}
		if n := query(t, db, "SELECT count(*) FROM items").Values[0][0]; n != int64(2) {
			t.Fatalf("batch did not roll back: %q: %v", second, n)
		}
	}
	if r, err := db.Transaction(context.Background(), nil); err != nil || r == nil || len(r) != 0 {
		t.Fatalf("empty batch: %v %v", r, err)
	}
}

func TestTransactionFunc(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tx.db")
	db := testDB(t, path)
	other := testDB(t, path)
	execute(t, db, "CREATE TABLE items (n UNIQUE)")
	execute(t, db, "INSERT INTO items VALUES (1)")
	ctx := context.Background()
	count := func(t *testing.T, db *DB) int64 {
		t.Helper()
		return query(t, db, "SELECT count(*) FROM items").Values[0][0].(int64)
	}

	// A write that depends on a read, seeing its own writes, isolated from
	// another connection until COMMIT.
	busy, err := Open(ctx, path, OpenOptions{BusyTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	err = db.TransactionFunc(ctx, func(tx *Tx) error {
		rows, err := tx.Query("SELECT max(n) FROM items")
		if err != nil {
			return err
		}
		next := rows.Values[0][0].(int64) + 1
		if r, err := tx.Execute("INSERT INTO items VALUES (?)", next); err != nil || r.Changes != 1 {
			return fmt.Errorf("insert: %v %v", r, err)
		}
		if rows, err := tx.Query("SELECT n FROM items ORDER BY n"); err != nil || !reflect.DeepEqual(rows.Values, [][]any{{int64(1)}, {int64(2)}}) {
			return fmt.Errorf("own writes: %v %v", rows, err)
		}
		if n := count(t, other); n != 1 {
			return fmt.Errorf("another connection read %d rows before COMMIT", n)
		}
		var sqliteErr *Error
		if _, err := busy.Execute(ctx, "INSERT INTO items VALUES (3)"); !errors.As(err, &sqliteErr) || sqliteErr.Code != 5 {
			return fmt.Errorf("another connection wrote during the transaction: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, other); n != 2 {
		t.Fatalf("committed rows = %d from another connection", n)
	}

	// Rollback: fn's error, a statement's, transaction control and
	// cancellation each roll back, returning the error as it was.
	mine := errors.New("decided not to")
	var stmtErr error
	failed := func(err error) bool { return err != nil }
	for _, c := range []struct {
		name string
		fn   func(tx *Tx, cancel func()) error
		want func(error) bool
	}{
		{"fn's error", func(*Tx, func()) error { return mine }, func(err error) bool { return err == mine }},
		{"statement error", func(tx *Tx, _ func()) error {
			_, stmtErr = tx.Execute("INSERT INTO items VALUES (1)")
			return stmtErr
		}, func(err error) bool {
			var e *Error
			return err != nil && err == stmtErr && errors.As(err, &e) && e.ExtendedCode == 2067
		}},
		{"COMMIT", func(tx *Tx, _ func()) error { _, err := tx.Execute("COMMIT"); return err }, failed},
		{"ROLLBACK", func(tx *Tx, _ func()) error { _, err := tx.Execute("ROLLBACK"); return err }, failed},
		{"BEGIN", func(tx *Tx, _ func()) error { _, err := tx.Query("BEGIN"); return err }, failed},
		{"SAVEPOINT", func(tx *Tx, _ func()) error { _, err := tx.Execute("SAVEPOINT escape"); return err }, failed},
		{"canceled", func(tx *Tx, cancel func()) error { cancel(); return nil }, func(err error) bool { return err == context.Canceled }},
		{"canceled statement", func(tx *Tx, cancel func()) error {
			cancel()
			_, err := tx.Execute("INSERT INTO items VALUES (101)")
			return err
		}, func(err error) bool { return err == context.Canceled }},
	} {
		ctx, cancel := context.WithCancel(ctx)
		err := db.TransactionFunc(ctx, func(tx *Tx) error {
			if _, err := tx.Execute("INSERT INTO items VALUES (100)"); err != nil {
				return err
			}
			return c.fn(tx, cancel)
		})
		cancel()
		if !c.want(err) {
			t.Errorf("%s: %v", c.name, err)
		}
		if n := count(t, db); n != 2 {
			t.Errorf("%s did not roll back: %d rows", c.name, n)
		}
	}
	// Cancellation interrupts the statement running, as in Transaction.
	timeout, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	err = db.TransactionFunc(timeout, func(tx *Tx) error {
		if _, err := tx.Execute("INSERT INTO items VALUES (100)"); err != nil {
			return err
		}
		_, err := tx.Query(longQuery)
		return err
	})
	if err != context.DeadlineExceeded {
		t.Errorf("interrupted statement: %v", err)
	}
	if n := count(t, db); n != 2 {
		t.Errorf("interrupted statement did not roll back: %d rows", n)
	}

	// A Tx used after fn returns, committed or rolled back.
	for _, result := range []error{nil, mine} {
		var kept *Tx
		if err := db.TransactionFunc(ctx, func(tx *Tx) error { kept = tx; return result }); err != result {
			t.Fatal(err)
		}
		if _, err := kept.Execute("INSERT INTO items VALUES (200)"); err != ErrTxDone {
			t.Fatalf("Execute after the transaction: %v", err)
		}
		if _, err := kept.Query("SELECT 1"); err != ErrTxDone {
			t.Fatalf("Query after the transaction: %v", err)
		}
	}
	if n := count(t, other); n != 2 {
		t.Fatalf("a done Tx wrote: %d rows", n)
	}

	// A panic in fn rolls back, and goes on.
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("recovered %v", r)
			}
		}()
		_ = db.TransactionFunc(ctx, func(tx *Tx) error {
			if _, err := tx.Execute("INSERT INTO items VALUES (300)"); err != nil {
				return err
			}
			panic("boom")
		})
	}()
	if n := count(t, db); n != 2 {
		t.Fatalf("panic did not roll back: %d rows", n)
	}
	execute(t, db, "INSERT INTO items VALUES (3)") // the connection was released
}

const longQuery = "WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT sum(x) FROM n"

func TestCancellationAndRollback(t *testing.T) {
	db := testDB(t, ":memory:")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := db.Query(ctx, longQuery); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	if n := query(t, db, "SELECT 42").Values[0][0]; n != int64(42) {
		t.Fatal(n)
	}
	execute(t, db, "CREATE TABLE items (n)")
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := db.Transaction(ctx, []Statement{{SQL: "INSERT INTO items VALUES (1)"}, {SQL: longQuery}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("batch cancellation: %v", err)
	}
	if n := query(t, db, "SELECT count(*) FROM items").Values[0][0]; n != int64(0) {
		t.Fatal("canceled batch did not roll back", n)
	}
}

func TestConcurrentOperations(t *testing.T) {
	db := testDB(t, ":memory:")
	execute(t, db, "CREATE TABLE counter(n)")
	execute(t, db, "INSERT INTO counter VALUES (0)")
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 8 {
				if _, err := db.Execute(context.Background(), "UPDATE counter SET n=n+1"); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if n := query(t, db, "SELECT n FROM counter").Values[0][0]; n != int64(128) {
		t.Fatal(n)
	}
}

func TestCloseAndQueuedCancellation(t *testing.T) {
	db := testDB(t, ":memory:")
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.operation(context.Background(), func(func()) error { close(started); _, _, err := db.run(longQuery, nil, true); return err })
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := db.Query(ctx, "SELECT 1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued cancellation: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(wait.Guard):
		t.Fatal("close did not interrupt query")
	}
	if _, err := db.Query(context.Background(), "SELECT 1"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiskReadOnlyAndBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.db")
	w := testDB(t, path)
	execute(t, w, "CREATE TABLE items(n)")
	execute(t, w, "INSERT INTO items VALUES (42)")
	r, err := Open(context.Background(), path, OpenOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if n := query(t, r, "SELECT n FROM items").Values[0][0]; n != int64(42) {
		t.Fatal(n)
	}
	if _, err := r.Execute(context.Background(), "DELETE FROM items"); err == nil {
		t.Fatal("read-only write succeeded")
	}
	b, err := Open(context.Background(), path, OpenOptions{BusyTimeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	locked, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- w.operation(context.Background(), func(func()) error {
			if err := w.control("BEGIN IMMEDIATE"); err != nil {
				close(locked)
				return err
			}
			close(locked)
			<-release
			return w.control("COMMIT")
		})
	}()
	<-locked
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	_, err = b.Execute(context.Background(), "INSERT INTO items VALUES (1)")
	var sqliteErr *Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code != 5 {
		t.Fatalf("busy: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := b.Execute(ctx, "INSERT INTO items VALUES (1)"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("busy cancellation: %v", err)
	}
}
