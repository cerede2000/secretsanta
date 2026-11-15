package reminders

import (
	"context"
	"testing"
	"time"

	"secretsanta/internal/config"
	"secretsanta/internal/db"
	"secretsanta/internal/draw"
	"secretsanta/internal/groups"
	"secretsanta/internal/letter"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/clock"
)

type zzLangs struct {
	lang string
	ok   bool
}

func (l zzLangs) DefaultLanguage(context.Context, int64) (string, bool, error) {
	return l.lang, l.ok, nil
}

func TestZZServiceDirectory(t *testing.T) {
	d, err := db.Open(t.TempDir())
	must(t, err)
	t.Cleanup(func() { _ = d.Close() })
	for _, m := range []struct {
		mod string
		fn  func() ([]db.Migration, error)
	}{{groups.Module, groups.Migrations}, {participants.Module, participants.Migrations},
		{letter.Module, letter.Migrations}, {draw.Module, draw.Migrations}, {Module, Migrations}} {
		ms, err := m.fn()
		must(t, err)
		must(t, db.Migrate(d, m.mod, ms))
	}
	clk := clock.NewFake(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
	gs, err := groups.NewService(d, clk, config.NewSecret("mdp-instance"))
	must(t, err)
	ctx := context.Background()
	r, err := gs.Create(ctx, groups.CreateInput{InstancePassword: "mdp-instance", ClientKey: "c", Name: "Famille", Slug: "famille"})
	must(t, err)
	gid := r.Group.ID
	ps := participants.NewService(d, clk, draw.NewState(d))
	_, err = ps.Create(ctx, participants.AdminActor(gid), gid, participants.Input{FirstName: "Ana", Name: "Zed"})
	must(t, err)

	dir := ServiceDirectory{DB: d, Groups: gs, People: ps}
	ids, err := dir.GroupIDs(ctx)
	if err != nil || len(ids) != 1 || ids[0] != gid {
		t.Fatalf("groupes : %v %v", ids, err)
	}
	info, err := dir.Group(ctx, gid)
	if err != nil || info.Slug != "famille" || info.Lang != "" {
		t.Fatalf("groupe : %+v %v", info, err)
	}
	dir.Langs = zzLangs{lang: "en", ok: true}
	if info, _ = dir.Group(ctx, gid); info.Lang != "en" {
		t.Fatalf("langue : %q", info.Lang)
	}
	if _, err := dir.Group(ctx, gid+50); err == nil {
		t.Error("groupe inconnu accepté")
	}
	list, err := dir.Participants(ctx, gid)
	if err != nil || len(list) != 1 {
		t.Fatalf("participants : %v %v", list, err)
	}

	f := SQLFacts{DB: d, Clock: clk}
	_ = d.Close()
	if _, err := dir.GroupIDs(ctx); err == nil {
		t.Error("GroupIDs sur base fermée")
	}
	if err := f.RecordLinkOpened(ctx, gid, 1); err == nil {
		t.Error("RecordLinkOpened sur base fermée")
	}
	if _, err := f.LinkOpened(ctx, gid, 1); err == nil {
		t.Error("LinkOpened sur base fermée")
	}
	if _, err := f.LetterWritten(ctx, gid, 1); err == nil {
		t.Error("LetterWritten sur base fermée")
	}
	if _, err := f.UnreadGivers(ctx, gid); err == nil {
		t.Error("UnreadGivers sur base fermée")
	}
}
