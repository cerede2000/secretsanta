package draw

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// onlyCounts vérifie que le type ne porte que des entiers et des booléens.
func onlyCounts(t *testing.T, ty reflect.Type, path string) {
	t.Helper()
	switch ty.Kind() {
	case reflect.Struct:
		for i := 0; i < ty.NumField(); i++ {
			onlyCounts(t, ty.Field(i).Type, path+"."+ty.Field(i).Name)
		}
	case reflect.Int, reflect.Bool:
	default:
		t.Fatalf("%s est de type %s : le rapport ne doit contenir que des comptes", path, ty)
	}
}

func TestCheckReportContainsOnlyCounts(t *testing.T) {
	onlyCounts(t, reflect.TypeOf(CheckReport{}), "CheckReport")
}

func TestCheckReportCountsAfterDraw(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "rapport-un", 0, nil)
	e.addN(t, gid, "Hortensia", 2, participants.RoleReceptionSeule)
	e.addN(t, gid, "Gaspardin", 1, participants.RoleOffrePlus)
	e.addN(t, gid, "Xavarin", 3, participants.RoleNormal)
	e.addN(t, gid, "Quentrix", 2, participants.RoleUnSeul)

	pre, err := e.svc.Check(ctx, admin(gid), gid, false)
	if err != nil {
		t.Fatal(err)
	}
	if pre.Drawn || pre.Beneficiaries != 8 || pre.Roles != (RoleCounts{ReceptionSeule: 2, OffrePlus: 1, Normal: 3, UnSeul: 2}) ||
		pre.Capacity != (CapacityLine{Cap: 2, ExtraGiftsNeeded: 2, OffrePlusCapacity: 1, TotalCapacity: 4}) || !pre.Green {
		t.Fatalf("contrôle avant tirage : %+v", pre)
	}

	e.mustDraw(t, gid)
	rep, err := e.svc.Report(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	want := CheckReport{
		Beneficiaries:          8,
		GiversOneGift:          4, // 6 tireurs, 2 cadeaux supplémentaires
		GiversTwoGifts:         2,
		Roles:                  RoleCounts{ReceptionSeule: 2, OffrePlus: 1, Normal: 3, UnSeul: 2},
		Capacity:               CapacityLine{Cap: 2, ExtraGiftsNeeded: 2, OffrePlusCapacity: 1, TotalCapacity: 4},
		EachReceivesExactlyOne: true,
		Drawn:                  true,
		Green:                  true,
	}
	if *rep != want {
		t.Fatalf("rapport %+v, attendu %+v", *rep, want)
	}
	b, _ := json.Marshal(rep)
	e.noNames(t, string(b))
	e.noNames(t, fmt.Sprintf("%+v", rep))

	_, err = e.svc.Report(ctx, who(gid, 1), gid)
	e.wantKind(t, err, errs.KindForbidden)
}

func TestCheckReportDetectsMissingGift(t *testing.T) {
	e := newEnv(t)
	gid := e.group(t, "rapport-deux", 0, nil)
	e.addN(t, gid, "Xavarin", 4, participants.RoleNormal)
	o := e.mustDraw(t, gid)
	if !o.Report.EachReceivesExactlyOne || !o.Report.Green {
		t.Fatalf("rapport du tirage : %+v", o.Report)
	}
	// Un participant arrivé après le tirage ne reçoit rien : rouge.
	e.add(t, gid, "Tardivel", participants.RoleNormal)
	rep, err := e.svc.Report(ctx, admin(gid), gid)
	if err != nil {
		t.Fatal(err)
	}
	if rep.EachReceivesExactlyOne || rep.Green {
		t.Fatalf("le contrôle doit être rouge : %+v", rep)
	}
}
