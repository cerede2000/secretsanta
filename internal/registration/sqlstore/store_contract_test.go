package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"path"
	"reflect"
	"sync"
	"testing"
	"time"

	"secretsanta/internal/participants"
	"secretsanta/internal/registration/code"
	"secretsanta/internal/registration/invite"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Équivalence avec les doublures en mémoire (propriétés).
// ---------------------------------------------------------------------------

func sameInvitation(a, b invite.Invitation) bool {
	if a.GroupID != b.GroupID || a.Enabled != b.Enabled || a.Capacity != b.Capacity || a.RoleChoice != b.RoleChoice ||
		!reflect.DeepEqual(a.TokenHash, b.TokenHash) || !a.UpdatedAt.Equal(b.UpdatedAt) {
		return false
	}
	if (a.ClosesAt == nil) != (b.ClosesAt == nil) {
		return false
	}
	return a.ClosesAt == nil || a.ClosesAt.Equal(*b.ClosesAt)
}

// samePending compare deux inscriptions ; l'identifiant SQL est traduit par
// ids (identifiant mémoire -> identifiant SQL).
func samePending(sqlP, memP invite.Pending, ids map[int64]int64) bool {
	if ids[memP.ID] != sqlP.ID {
		return false
	}
	a, b := sqlP, memP
	a.ID, b.ID = 0, 0
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.ExpiresAt.Equal(b.ExpiresAt) {
		return false
	}
	a.CreatedAt, b.CreatedAt, a.ExpiresAt, b.ExpiresAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return a == b
}

// Propriété : sur toute suite d'opérations, le magasin SQL de l'invitation et
// des inscriptions répond exactement comme invite.MemoryStore (renouvellement
// du condensé, plafond et clôture, inscriptions vivantes, unicité du nom et
// de l'adresse, isolation des groupes, expiration à 48 heures). Une purge à
// l'instant courant n'a aucun effet visible.
func TestInviteStoreMatchesMemoryStoreProperty(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t, t.TempDir())
	st := NewInviteStore(d)
	gen := 0
	rapid.Check(t, func(rt *rapid.T) {
		gen++
		groups := []int64{insertGroup(t, d, fmt.Sprintf("p%da", gen)), insertGroup(t, d, fmt.Sprintf("p%db", gen))}
		ref := invite.NewMemoryStore()
		ids := map[int64]int64{}
		var memIDs []int64
		now := t0
		names := []string{"lea martin", "hugo bernard", "camille m"}
		emails := []string{"lea@example.org", "hugo@example.org"}
		for i, n := 0, rapid.IntRange(1, 40).Draw(rt, "ops"); i < n; i++ {
			g := rapid.SampledFrom(groups).Draw(rt, "group")
			switch rapid.IntRange(0, 7).Draw(rt, "op") {
			case 0: // enregistrer l'invitation (activation, renouvellement, réglages)
				inv := invite.Invitation{
					GroupID: g, Enabled: rapid.Bool().Draw(rt, "enabled"),
					Capacity:   rapid.IntRange(0, 50).Draw(rt, "capacity"),
					RoleChoice: rapid.Bool().Draw(rt, "roleChoice"),
					UpdatedAt:  now,
				}
				if rapid.Bool().Draw(rt, "hash") {
					inv.TokenHash = rawHash(rapid.SampledFrom([]string{"t1", "t2", "t3"}).Draw(rt, "token"))
				}
				if rapid.Bool().Draw(rt, "closes") {
					c := now.Add(time.Duration(rapid.IntRange(-48, 48).Draw(rt, "closesIn")) * time.Hour)
					inv.ClosesAt = &c
				}
				if err := st.SaveInvitation(ctx, inv); err != nil {
					rt.Fatal(err)
				}
				if err := ref.SaveInvitation(ctx, inv); err != nil {
					rt.Fatal(err)
				}
			case 1:
				got, err := st.Invitation(ctx, g)
				if err != nil {
					rt.Fatal(err)
				}
				want, _ := ref.Invitation(ctx, g)
				if !sameInvitation(got, want) {
					rt.Fatalf("Invitation(%d) : SQL %+v, référence %+v", g, got, want)
				}
			case 2: // nouvelle inscription
				nameKey := rapid.SampledFrom(names).Draw(rt, "name")
				email := rapid.SampledFrom(emails).Draw(rt, "email")
				p := invite.Pending{
					GroupID: g, Ref: fmt.Sprintf("ref-%d-%d", gen, i), FirstName: "Prénom", Name: nameKey,
					NameKey: nameKey, Email: email, EmailKey: email,
					SharedAddress: rapid.Bool().Draw(rt, "shared"),
					Lang:          rapid.SampledFrom([]string{"", "fr", "en"}).Draw(rt, "lang"),
					Role: rapid.SampledFrom([]participants.DrawRole{participants.RoleNormal,
						participants.RoleReceptionSeule, participants.RoleOffrePlus}).Draw(rt, "role"),
					CreatedAt: now, ExpiresAt: now.Add(invite.PendingTTL),
				}
				sp, err := st.AddPending(ctx, p)
				if err != nil {
					rt.Fatal(err)
				}
				mp, _ := ref.AddPending(ctx, p)
				ids[mp.ID] = sp.ID
				memIDs = append(memIDs, mp.ID)
				if !samePending(sp, mp, ids) {
					rt.Fatalf("AddPending : SQL %+v, référence %+v", sp, mp)
				}
			case 3: // suppression, éventuellement depuis le mauvais groupe
				if len(memIDs) == 0 {
					continue
				}
				id := rapid.SampledFrom(memIDs).Draw(rt, "delete")
				if err := st.DeletePending(ctx, g, ids[id]); err != nil {
					rt.Fatal(err)
				}
				_ = ref.DeletePending(ctx, g, id)
			case 4: // le temps passe (jusqu'à dépasser 48 heures)
				now = now.Add(time.Duration(rapid.IntRange(1, 30).Draw(rt, "hours")) * time.Hour)
			case 5:
				got, err := st.ListPending(ctx, g, now)
				if err != nil {
					rt.Fatal(err)
				}
				want, _ := ref.ListPending(ctx, g, now)
				if len(got) != len(want) {
					rt.Fatalf("ListPending : SQL %d, référence %d", len(got), len(want))
				}
				for j := range got {
					if !samePending(got[j], want[j], ids) {
						rt.Fatalf("ListPending[%d] : SQL %+v, référence %+v", j, got[j], want[j])
					}
				}
				n, err := st.CountPending(ctx, g, now)
				if err != nil {
					rt.Fatal(err)
				}
				if m, _ := ref.CountPending(ctx, g, now); n != m {
					rt.Fatalf("CountPending : SQL %d, référence %d", n, m)
				}
			case 6:
				key := rapid.SampledFrom(names).Draw(rt, "probeName")
				got, err := st.PendingNameExists(ctx, g, key, now)
				if err != nil {
					rt.Fatal(err)
				}
				if want, _ := ref.PendingNameExists(ctx, g, key, now); got != want {
					rt.Fatalf("PendingNameExists(%q) : SQL %v, référence %v", key, got, want)
				}
				mail := rapid.SampledFrom(emails).Draw(rt, "probeEmail")
				got, err = st.PendingEmailExists(ctx, g, mail, now)
				if err != nil {
					rt.Fatal(err)
				}
				if want, _ := ref.PendingEmailExists(ctx, g, mail, now); got != want {
					rt.Fatalf("PendingEmailExists(%q) : SQL %v, référence %v", mail, got, want)
				}
			case 7: // purge à l'instant courant : rien de vivant ne disparaît
				if _, err := st.PurgeExpired(ctx, now); err != nil {
					rt.Fatal(err)
				}
			}
		}
	})
}

// Propriété : sur toute suite d'opérations, le magasin SQL des codes répond
// exactement comme code.MemoryStore (remplacement, tentatives, consommation
// conditionnelle, expiration, isolation des groupes).
func TestCodeStoreMatchesMemoryStoreProperty(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t, t.TempDir())
	st := NewCodeStore(d)
	gen := 0
	rapid.Check(t, func(rt *rapid.T) {
		gen++
		groups := []int64{insertGroup(t, d, fmt.Sprintf("c%da", gen)), insertGroup(t, d, fmt.Sprintf("c%db", gen))}
		ref := code.NewMemoryStore()
		hashes := []string{hexHash("a"), hexHash("b"), hexHash("c")}
		regs := []int64{1, 2}
		for i, n := 0, rapid.IntRange(1, 40).Draw(rt, "ops"); i < n; i++ {
			g := rapid.SampledFrom(groups).Draw(rt, "group")
			reg := rapid.SampledFrom(regs).Draw(rt, "reg")
			h := rapid.SampledFrom(hashes).Draw(rt, "hash")
			now := t0.Add(time.Duration(rapid.IntRange(0, 20).Draw(rt, "minutes")) * time.Minute)
			switch rapid.IntRange(0, 5).Draw(rt, "op") {
			case 0:
				r := code.Record{GroupID: g, RegistrationID: reg, CodeHash: h, TokenHash: tokenOf(reg, h),
					IssuedAt: t0, ExpiresAt: t0.Add(code.TTL)}
				if err := st.Put(ctx, r); err != nil {
					rt.Fatal(err)
				}
				_ = ref.Put(ctx, r)
			case 1:
				got, okS, err := st.Get(ctx, g, reg)
				if err != nil {
					rt.Fatal(err)
				}
				want, okM, _ := ref.Get(ctx, g, reg)
				if okS != okM || !sameRecord(got, want) {
					rt.Fatalf("Get : SQL %+v/%v, référence %+v/%v", got, okS, want, okM)
				}
			case 2:
				th := tokenOf(reg, h)
				got, okS, err := st.FindByToken(ctx, g, th)
				if err != nil {
					rt.Fatal(err)
				}
				want, okM, _ := ref.FindByToken(ctx, g, th)
				if okS != okM || !sameRecord(got, want) {
					rt.Fatalf("FindByToken : SQL %+v/%v, référence %+v/%v", got, okS, want, okM)
				}
			case 3:
				nS, okS, err := st.AddAttempt(ctx, g, reg, h)
				if err != nil {
					rt.Fatal(err)
				}
				nM, okM, _ := ref.AddAttempt(ctx, g, reg, h)
				if nS != nM || okS != okM {
					rt.Fatalf("AddAttempt : SQL %d/%v, référence %d/%v", nS, okS, nM, okM)
				}
			case 4:
				max := rapid.IntRange(1, code.MaxAttempts).Draw(rt, "max")
				okS, err := st.Consume(ctx, g, reg, h, now, max)
				if err != nil {
					rt.Fatal(err)
				}
				okM, _ := ref.Consume(ctx, g, reg, h, now, max)
				if okS != okM {
					rt.Fatalf("Consume(%v, %d) : SQL %v, référence %v", now, max, okS, okM)
				}
			case 5:
				if err := st.Delete(ctx, g, reg); err != nil {
					rt.Fatal(err)
				}
				_ = ref.Delete(ctx, g, reg)
			}
		}
	})
}

// tokenOf donne une empreinte de jeton propre à une inscription : deux
// inscriptions ne partagent jamais un jeton (128 bits aléatoires en vrai), et
// la doublure en mémoire n'ordonne pas ses recherches.
func tokenOf(reg int64, codeHash string) string {
	return hexHash(fmt.Sprintf("tok%d%s", reg, codeHash))
}

func sameRecord(a, b code.Record) bool {
	if !a.IssuedAt.Equal(b.IssuedAt) || !a.ExpiresAt.Equal(b.ExpiresAt) {
		return false
	}
	a.IssuedAt, b.IssuedAt, a.ExpiresAt, b.ExpiresAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	return a == b
}

// Le contrat explicite du magasin des codes (celui de TestMemoryStoreContract
// de internal/registration/code), rejoué sur le magasin SQL.
func TestCodeStoreContract(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t, t.TempDir())
	g1, g2 := insertGroup(t, d, "noel"), insertGroup(t, d, "famille")
	s := NewCodeStore(d)
	a, b, other := hexHash("a"), hexHash("b"), hexHash("autre")
	put := func(hash string) {
		t.Helper()
		if err := s.Put(ctx, code.Record{GroupID: g1, RegistrationID: 1, CodeHash: hash, TokenHash: hexHash("t" + hash),
			IssuedAt: t0, ExpiresAt: t0.Add(code.TTL)}); err != nil {
			t.Fatal(err)
		}
	}
	put(a)
	if _, ok, _ := s.FindByToken(ctx, g2, hexHash("t"+a)); ok {
		t.Fatal("jeton trouvé dans un autre groupe")
	}
	if _, ok, _ := s.Get(ctx, g2, 1); ok {
		t.Fatal("code trouvé dans un autre groupe")
	}
	if r, ok, _ := s.FindByToken(ctx, g1, hexHash("t"+a)); !ok || r.RegistrationID != 1 || r.GroupID != g1 {
		t.Fatal("jeton introuvable")
	}
	if _, ok, _ := s.AddAttempt(ctx, g1, 1, other); ok {
		t.Fatal("tentative comptée sur un code remplacé")
	}
	if _, ok, _ := s.AddAttempt(ctx, g2, 1, a); ok {
		t.Fatal("tentative comptée dans un autre groupe")
	}
	for i := 1; i <= 3; i++ {
		if n, ok, _ := s.AddAttempt(ctx, g1, 1, a); !ok || n != i {
			t.Fatalf("tentative %d : %d %v", i, n, ok)
		}
	}
	if ok, _ := s.Consume(ctx, g1, 1, a, t0, 2); ok {
		t.Fatal("consommé au-delà des saisies permises")
	}
	if ok, _ := s.Consume(ctx, g1, 1, a, t0.Add(code.TTL), 5); ok {
		t.Fatal("consommé à l'expiration (15 minutes)")
	}
	if ok, _ := s.Consume(ctx, g1, 1, other, t0, 5); ok {
		t.Fatal("consommé avec une autre empreinte")
	}
	if ok, _ := s.Consume(ctx, g2, 1, a, t0, 5); ok {
		t.Fatal("consommé depuis un autre groupe")
	}
	if ok, _ := s.Consume(ctx, g1, 1, a, t0.Add(code.TTL-time.Nanosecond), 3); !ok {
		t.Fatal("consommation refusée juste avant l'expiration")
	}
	if ok, _ := s.Consume(ctx, g1, 1, a, t0, 5); ok {
		t.Fatal("consommé deux fois")
	}
	put(b)
	if _, ok, _ := s.FindByToken(ctx, g1, hexHash("t"+a)); ok {
		t.Fatal("ancien jeton encore trouvé")
	}
	if r, _, _ := s.Get(ctx, g1, 1); r.Attempts != 0 || r.Consumed || r.CodeHash != b {
		t.Fatalf("remplacement %+v", r)
	}
	if err := s.Delete(ctx, g1, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, g1, 1); ok {
		t.Fatal("code non supprimé")
	}
	if err := s.Delete(ctx, g1, 1); err != nil {
		t.Fatal("suppression d'un code absent")
	}
}

// Saisies simultanées : chaque tentative est comptée une fois et une seule
// consommation réussit.
func TestCodeStoreConcurrentAttemptsAndConsume(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t, t.TempDir())
	g := insertGroup(t, d, "noel")
	s := NewCodeStore(d)
	h := hexHash("code")
	if err := s.Put(ctx, code.Record{GroupID: g, RegistrationID: 7, CodeHash: h, TokenHash: hexHash("tok"),
		IssuedAt: t0, ExpiresAt: t0.Add(code.TTL)}); err != nil {
		t.Fatal(err)
	}
	const n = 12
	var wg sync.WaitGroup
	seen := make(chan int, n)
	wins := make(chan bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, ok, err := s.AddAttempt(ctx, g, 7, h)
			if err != nil || !ok {
				t.Errorf("tentative : %d %v %v", c, ok, err)
			}
			seen <- c
			w, err := s.Consume(ctx, g, 7, h, t0, 1000)
			if err != nil {
				t.Errorf("consommation : %v", err)
			}
			wins <- w
		}()
	}
	wg.Wait()
	close(seen)
	close(wins)
	counts := map[int]bool{}
	for c := range seen {
		if counts[c] {
			t.Fatalf("compteur %d rendu deux fois", c)
		}
		counts[c] = true
	}
	if len(counts) != n {
		t.Fatalf("%d valeurs de compteur, attendu %d", len(counts), n)
	}
	won := 0
	for w := range wins {
		if w {
			won++
		}
	}
	if won != 1 {
		t.Fatalf("%d consommations réussies, attendu 1", won)
	}
}

// ---------------------------------------------------------------------------
// Contrats des services réels sur les magasins SQL.
// ---------------------------------------------------------------------------

// Renouveler le lien remplace le condensé en base : l'ancien jeton donne la
// réponse neutre, le nouveau ouvre le formulaire, y compris pour un magasin
// rouvert sur la même base. Désactiver efface le condensé.
func TestInvitationRenewalOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	first := e.enable(t, e.g1)
	if _, err := e.svc.Open(ctx, "noel", first.Token); err != nil {
		t.Fatalf("jeton initial refusé : %v", err)
	}
	inv, err := e.invites.Invitation(ctx, e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Enabled || len(inv.TokenHash) != 32 {
		t.Fatalf("invitation enregistrée %+v", inv)
	}
	renewed, err := e.svc.Renew(ctx, adminOf(e.g1), e.g1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Open(ctx, "noel", first.Token); !errors.Is(err, invite.ErrNeutral) {
		t.Fatalf("ancien jeton après renouvellement : %v, attendu la réponse neutre", err)
	}
	if _, err := e.svc.Open(ctx, "noel", renewed.Token); err != nil {
		t.Fatalf("nouveau jeton refusé : %v", err)
	}
	if _, err := e.svc.Open(ctx, "famille", renewed.Token); !errors.Is(err, invite.ErrNeutral) {
		t.Fatalf("jeton accepté dans un autre groupe : %v", err)
	}
	// Persistance : un magasin rouvert lit le même condensé.
	again, err := NewInviteStore(e.db).Invitation(ctx, e.g1)
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := e.invites.Invitation(ctx, e.g1)
	if !sameInvitation(again, cur) || reflect.DeepEqual(cur.TokenHash, inv.TokenHash) {
		t.Fatalf("condensé non renouvelé en base : %+v / %+v", inv, cur)
	}
	// Plafond et clôture enregistrés et relus.
	closes := t0.Add(72 * time.Hour)
	if err := e.svc.SetLimits(ctx, adminOf(e.g1), e.g1, 12, &closes); err != nil {
		t.Fatal(err)
	}
	cur, _ = e.invites.Invitation(ctx, e.g1)
	if cur.Capacity != 12 || cur.ClosesAt == nil || !cur.ClosesAt.Equal(closes) || !cur.Enabled {
		t.Fatalf("réglages relus %+v", cur)
	}
	if err := e.svc.Disable(ctx, adminOf(e.g1), e.g1); err != nil {
		t.Fatal(err)
	}
	cur, _ = e.invites.Invitation(ctx, e.g1)
	if cur.Enabled || cur.TokenHash != nil {
		t.Fatalf("désactivation : %+v", cur)
	}
	if _, err := e.svc.Open(ctx, "noel", renewed.Token); !errors.Is(err, invite.ErrNeutral) {
		t.Fatalf("jeton accepté après désactivation : %v", err)
	}
}

// register inscrit une personne et renvoie son inscription et son courriel.
func (e *env) register(t *testing.T, g int64, slug, tok, first, name, email string) (invite.Pending, code.Mail) {
	t.Helper()
	r := e.submit(t, slug, tok, submission(first, name, email))
	if r.Outcome != invite.OutcomeCodeSent {
		t.Fatalf("inscription de %s : %s", first, r.Outcome)
	}
	return e.pendingByRef(t, g, r.Ref), e.mail.last(t)
}

// Cinq tentatives par code : un bon code à la 5e saisie passe, après cinq
// saisies fausses il est refusé. Le compteur est bien celui de la base.
func TestFiveAttemptsOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	l := e.enable(t, e.g1)
	pa, ma := e.register(t, e.g1, "noel", l.Token, "Léa", "Martin", "lea@example.org")
	pb, mb := e.register(t, e.g1, "noel", l.Token, "Hugo", "Bernard", "hugo@example.org")
	wrong := func(c string) string {
		if c == "222222" {
			return "333333"
		}
		return "222222"
	}
	for i := 0; i < code.MaxAttempts-1; i++ {
		if _, err := e.codeSvc.Verify(ctx, e.g1, pa.ID, wrong(ma.Code)); !code.IsRefused(err) {
			t.Fatalf("code faux accepté : %v", err)
		}
	}
	if r, err := e.codeSvc.Verify(ctx, e.g1, pa.ID, ma.Code); err != nil || r.RegistrationID != pa.ID {
		t.Fatalf("bon code à la 5e saisie refusé : %v", err)
	}
	for i := 0; i < code.MaxAttempts; i++ {
		if _, err := e.codeSvc.Verify(ctx, e.g1, pb.ID, wrong(mb.Code)); !code.IsRefused(err) {
			t.Fatalf("code faux accepté : %v", err)
		}
	}
	if _, err := e.codeSvc.Verify(ctx, e.g1, pb.ID, mb.Code); !code.IsRefused(err) {
		t.Fatalf("bon code accepté après 5 saisies fausses : %v", err)
	}
	if _, err := e.codeSvc.Preview(ctx, e.g1, path.Base(mb.Link)); !code.IsRefused(err) {
		t.Fatalf("lien de confirmation accepté après 5 saisies : %v", err)
	}
	rec, ok, err := e.codes.Get(ctx, e.g1, pb.ID)
	if err != nil || !ok || rec.Attempts != code.MaxAttempts+1 || rec.Consumed {
		t.Fatalf("compteur en base %+v %v %v", rec, ok, err)
	}
	// Une inscription d'un autre groupe ne voit pas ce code.
	if _, err := e.codeSvc.Verify(ctx, e.g2, pa.ID, ma.Code); !code.IsRefused(err) {
		t.Fatalf("code vérifié dans un autre groupe : %v", err)
	}
}

// Le code et son lien expirent à 15 minutes exactement (horloge simulée).
func TestCodeExpiresAfterFifteenMinutesOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	l := e.enable(t, e.g1)
	pa, ma := e.register(t, e.g1, "noel", l.Token, "Léa", "Martin", "lea@example.org")
	pb, mb := e.register(t, e.g1, "noel", l.Token, "Hugo", "Bernard", "hugo@example.org")
	e.clock.Advance(code.TTL - time.Nanosecond)
	if _, err := e.codeSvc.Preview(ctx, e.g1, path.Base(ma.Link)); err != nil {
		t.Fatalf("lien refusé avant 15 minutes : %v", err)
	}
	if _, err := e.codeSvc.Verify(ctx, e.g1, pa.ID, ma.Code); err != nil {
		t.Fatalf("code refusé avant 15 minutes : %v", err)
	}
	e.clock.Advance(time.Nanosecond)
	if _, err := e.codeSvc.Verify(ctx, e.g1, pb.ID, mb.Code); !code.IsRefused(err) {
		t.Fatalf("code accepté à 15 minutes : %v", err)
	}
	if _, err := e.codeSvc.Confirm(ctx, e.g1, path.Base(mb.Link)); !code.IsRefused(err) {
		t.Fatalf("lien accepté à 15 minutes : %v", err)
	}
}

// Purge à 48 heures (horloge simulée) : avant l'échéance l'inscription est
// comptée et rien n'est purgé ; à l'échéance elle n'est plus comptée, la
// purge la retire avec son code, qui ne fonctionne plus ; les inscriptions
// plus récentes et celles des autres groupes restent ; rejouer est sans effet.
func TestPurgeAfterFortyEightHoursOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	l1, l2 := e.enable(t, e.g1), e.enable(t, e.g2)
	old, oldMail := e.register(t, e.g1, "noel", l1.Token, "Léa", "Martin", "lea@example.org")
	e.clock.Advance(time.Hour)
	young, _ := e.register(t, e.g1, "noel", l1.Token, "Hugo", "Bernard", "hugo@example.org")
	e.clock.Advance(-time.Hour)
	e.clock.Advance(time.Minute)
	other, _ := e.register(t, e.g2, "famille", l2.Token, "Léa", "Martin", "lea@example.org")
	e.clock.Set(t0)

	e.clock.Set(old.CreatedAt.Add(invite.PendingTTL - time.Nanosecond))
	if n, err := e.invites.PurgeExpired(ctx, e.clock.Now()); err != nil || n != 0 {
		t.Fatalf("purge avant 48 heures : %d %v", n, err)
	}
	if n, _ := e.invites.CountPending(ctx, e.g1, e.clock.Now()); n != 2 {
		t.Fatalf("%d inscriptions comptées avant 48 heures, attendu 2", n)
	}

	e.clock.Set(old.CreatedAt.Add(invite.PendingTTL))
	if n, _ := e.invites.CountPending(ctx, e.g1, e.clock.Now()); n != 1 {
		t.Fatalf("%d inscriptions comptées à 48 heures, attendu 1", n)
	}
	if n, err := e.invites.PurgeExpired(ctx, e.clock.Now()); err != nil || n != 1 {
		t.Fatalf("purge à 48 heures : %d %v, attendu 1", n, err)
	}
	var rows int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM registration_pending WHERE id = ?`, old.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("inscription expirée encore en base : %d %v", rows, err)
	}
	if _, ok, _ := e.codes.Get(ctx, e.g1, old.ID); ok {
		t.Fatal("code de l'inscription purgée encore en base")
	}
	if _, err := e.codeSvc.Verify(ctx, e.g1, old.ID, oldMail.Code); !code.IsRefused(err) {
		t.Fatalf("code d'une inscription purgée accepté : %v", err)
	}
	if _, ok, _ := e.codes.Get(ctx, e.g1, young.ID); !ok {
		t.Fatal("code d'une inscription vivante purgé")
	}
	if _, ok, _ := e.codes.Get(ctx, e.g2, other.ID); !ok {
		t.Fatal("code d'un autre groupe purgé")
	}
	if n, _ := e.invites.CountPending(ctx, e.g2, e.clock.Now()); n != 1 {
		t.Fatalf("autre groupe : %d inscriptions, attendu 1", n)
	}
	if n, err := e.invites.PurgeExpired(ctx, e.clock.Now()); err != nil || n != 0 {
		t.Fatalf("rejeu de la purge : %d %v", n, err)
	}
	// Plus tard, tout est purgé.
	e.clock.Advance(invite.PendingTTL)
	if n, err := e.invites.PurgeExpired(ctx, e.clock.Now()); err != nil || n != 2 {
		t.Fatalf("purge finale : %d %v, attendu 2", n, err)
	}
	if countRows(t, e.db, "registration_pending") != 0 || countRows(t, e.db, "registration_codes") != 0 {
		t.Fatal("des lignes restent après la purge finale")
	}
	// Un identifiant d'inscription n'est jamais réattribué.
	again, _ := e.register(t, e.g1, "noel", l1.Token, "Léa", "Martin", "lea@example.org")
	for _, id := range []int64{old.ID, young.ID, other.ID} {
		if again.ID == id {
			t.Fatalf("identifiant %d réattribué", id)
		}
	}
}

// Unicité du nom sur le magasin SQL : une inscription en attente bloque le
// même prénom et nom, quelles que soient la casse et les accents, dans son
// groupe seulement et jusqu'à son expiration.
func TestNameUnicityOverSQL(t *testing.T) {
	e := newEnv(t)
	l1, l2 := e.enable(t, e.g1), e.enable(t, e.g2)
	e.register(t, e.g1, "noel", l1.Token, "Léa", "Martin", "lea@example.org")
	sent := len(e.mail.sent)
	r := e.submit(t, "noel", l1.Token, submission("LEA", "martin", "autre@example.org"))
	if r.Outcome != invite.OutcomeNameTaken {
		t.Fatalf("doublon sans casse ni accents : %s", r.Outcome)
	}
	if len(e.mail.sent) != sent {
		t.Fatal("un code est parti pour un nom déjà inscrit")
	}
	if n, _ := e.invites.CountPending(t.Context(), e.g1, e.clock.Now()); n != 1 {
		t.Fatalf("%d inscriptions, attendu 1", n)
	}
	if r := e.submit(t, "famille", l2.Token, submission("Lea", "Martin", "lea@example.org")); r.Outcome != invite.OutcomeCodeSent {
		t.Fatalf("même nom dans un autre groupe : %s", r.Outcome)
	}
	e.clock.Advance(invite.PendingTTL)
	if r := e.submit(t, "noel", l1.Token, submission("Léa", "Martin", "lea@example.org")); r.Outcome != invite.OutcomeCodeSent {
		t.Fatalf("nom toujours pris après expiration : %s", r.Outcome)
	}
}

// Adresse partagée sur le magasin SQL : une adresse déjà en attente (sans
// casse) demande confirmation et rien ne part ; confirmée, le code part à
// cette adresse et la confirmation est enregistrée.
func TestSharedAddressConfirmedOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	l := e.enable(t, e.g1)
	first, _ := e.register(t, e.g1, "noel", l.Token, "Léa", "Martin", "lea@example.org")
	if first.SharedAddress {
		t.Fatal("première inscription marquée adresse partagée")
	}
	sent := len(e.mail.sent)
	r := e.submit(t, "noel", l.Token, submission("Hugo", "Martin", "LEA@example.org"))
	if r.Outcome != invite.OutcomeConfirmShared {
		t.Fatalf("adresse connue sans confirmation : %s", r.Outcome)
	}
	if len(e.mail.sent) != sent {
		t.Fatal("un code est parti sans confirmation")
	}
	if n, _ := e.invites.CountPending(ctx, e.g1, e.clock.Now()); n != 1 {
		t.Fatalf("%d inscriptions, attendu 1", n)
	}
	s := submission("Hugo", "Martin", "LEA@example.org")
	s.ConfirmShared = true
	r = e.submit(t, "noel", l.Token, s)
	if r.Outcome != invite.OutcomeCodeSent {
		t.Fatalf("adresse confirmée : %s", r.Outcome)
	}
	if m := e.mail.last(t); m.To != "LEA@example.org" || m.FirstName != "Hugo" {
		t.Fatalf("code envoyé à %q pour %q", m.To, m.FirstName)
	}
	p := e.pendingByRef(t, e.g1, r.Ref)
	if !p.SharedAddress || p.EmailKey != "lea@example.org" {
		t.Fatalf("inscription enregistrée %+v", p)
	}
	// La même adresse est inconnue dans un autre groupe.
	if ok, _ := e.invites.PendingEmailExists(ctx, e.g2, "lea@example.org", e.clock.Now()); ok {
		t.Fatal("adresse visible depuis un autre groupe")
	}
}

// Suppression d'une inscription : seulement dans son groupe.
func TestDeletePendingIsGroupScopedOverSQL(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	l := e.enable(t, e.g1)
	p, _ := e.register(t, e.g1, "noel", l.Token, "Léa", "Martin", "lea@example.org")
	if err := e.invites.DeletePending(ctx, e.g2, p.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.invites.CountPending(ctx, e.g1, e.clock.Now()); n != 1 {
		t.Fatal("inscription supprimée depuis un autre groupe")
	}
	if err := e.invites.DeletePending(ctx, e.g1, p.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.invites.CountPending(ctx, e.g1, e.clock.Now()); n != 0 {
		t.Fatal("inscription non supprimée")
	}
	if _, err := e.invites.AddPending(ctx, invite.Pending{}); err == nil {
		t.Fatal("inscription sans groupe acceptée")
	}
	if err := e.invites.SaveInvitation(ctx, invite.Invitation{}); err == nil {
		t.Fatal("invitation sans groupe acceptée")
	}
}
