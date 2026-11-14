package infrarecurrence

import (
	"errors"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domainrecurrence "github.com/pluto-han/taskflow/internal/domain/recurrence"
)

func parseID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if e := id.Scan(raw); e != nil || !id.Valid {
		return id, domainrecurrence.ErrInvalid
	}

	return id, nil
}

func date(raw string) pgtype.Date {
	d, e := domainplanning.ParseDate(raw)

	return pgtype.Date{Time: d, Valid: e == nil}
}

func domain(row db.RecurrenceRule, e error) (*domainrecurrence.Rule, error) {
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, domainrecurrence.ErrNotFound
	}
	if e != nil {
		return nil, e
	}

	r := &domainrecurrence.Rule{
		ReminderEnabled: row.ReminderEnabled,
		ID:              row.ID.String(),
		OwnerID:         domainaccount.UserID(row.OwnerID.String()),
		Title:           row.Title,
		Description:     row.Description,
		Priority:        int(row.Priority),
		AnchorDate:      row.AnchorDate.Time.Format(time.DateOnly),
		IntervalDays:    int(row.IntervalDays),
		NextRunDate:     row.NextRunDate.Time.Format(time.DateOnly),
		Paused:          row.Paused,
		Version:         row.Version,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}
	if row.DeletedAt.Valid {
		r.DeletedAt = &row.DeletedAt.Time
	}
	return r, nil
}
