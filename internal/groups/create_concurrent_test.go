package groups

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// Des créations simultanées de groupes distincts doivent toutes réussir : la
// transaction de création lit (unicité du slug) puis écrit ; sous WAL, une
// transaction différée qui passe de la lecture à l'écriture après la validation
// d'un autre écrivain échoue aussitôt en « database is locked », sans que
// busy_timeout intervienne.
func TestCreateConcurrentDistinctGroups(t *testing.T) {
	e := newEnv(t)
	const n = 40
	var wg sync.WaitGroup
	errc := make(chan error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			in := validInput(fmt.Sprintf("groupe-concurrent-%d", i))
			in.ClientKey = fmt.Sprintf("198.51.100.%d", i+1)
			if _, err := e.svc.Create(context.Background(), in); err != nil {
				errc <- fmt.Errorf("création %d : %w (cause : %v)", i, err, errors.Unwrap(err))
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
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM groups`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != n {
		t.Errorf("%d groupes créés, %d attendus", count, n)
	}
}
