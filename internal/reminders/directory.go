package reminders

import (
	"context"
	"database/sql"

	"secretsanta/internal/groups"
	"secretsanta/internal/participants"
	"secretsanta/internal/shared/errs"
)

// GroupLangs donne la langue par défaut d'un groupe (*groupsettings.Service).
type GroupLangs interface {
	DefaultLanguage(ctx context.Context, groupID int64) (lang string, ok bool, err error)
}

// ServiceDirectory est le Directory branché sur les services de F2 et F3.
type ServiceDirectory struct {
	DB     *sql.DB
	Groups *groups.Service
	People *participants.Service
	Langs  GroupLangs // facultatif
}

// GroupIDs liste tous les groupes.
func (d ServiceDirectory) GroupIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.DB.QueryContext(ctx, `SELECT id FROM groups ORDER BY id`)
	if err != nil {
		return nil, errs.Internal(err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, errs.Internal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.Internal(err)
	}
	return out, nil
}

// Group renvoie le nom, le slug et la langue du groupe.
func (d ServiceDirectory) Group(ctx context.Context, groupID int64) (GroupInfo, error) {
	g, err := d.Groups.ByID(ctx, groupID)
	if err != nil {
		return GroupInfo{}, err
	}
	info := GroupInfo{ID: g.ID, Slug: g.Slug, Name: g.Name}
	if d.Langs != nil {
		if l, ok, err := d.Langs.DefaultLanguage(ctx, groupID); err == nil && ok {
			info.Lang = l
		}
	}
	return info, nil
}

// Participants liste les participants du groupe (vue complète).
func (d ServiceDirectory) Participants(ctx context.Context, groupID int64) ([]participants.Participant, error) {
	return d.People.List(ctx, participants.AdminActor(groupID), groupID)
}
