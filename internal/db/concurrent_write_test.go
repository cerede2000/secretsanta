package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// Toute transaction de l'application lit puis écrit. Sous WAL, une transaction
// différée dont la lecture précède la validation d'un autre écrivain échoue en
// « database is locked » dès son premier ordre d'écriture, sans attendre
// busy_timeout : Open doit donc ouvrir les transactions en BEGIN IMMEDIATE.
func TestConcurrentReadThenWriteTransactions(t *testing.T) {
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	const n = 40
	var wg sync.WaitGroup
	errc := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ctx := context.Background()
			tx, err := d.BeginTx(ctx, nil)
			if err != nil {
				errc <- err
				return
			}
			defer func() { _ = tx.Rollback() }()
			var c int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM t WHERE v = ?`, fmt.Sprint(i)).Scan(&c); err != nil {
				errc <- err
				return
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES (?)`, fmt.Sprint(i)); err != nil {
				errc <- fmt.Errorf("écriture %d : %w", i, err)
				return
			}
			if err := tx.Commit(); err != nil {
				errc <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Error(err)
	}
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != n {
		t.Errorf("%d lignes, %d attendues", count, n)
	}
}
