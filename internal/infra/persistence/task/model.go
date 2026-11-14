package infratask

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

func toPgTimestamp(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{Valid: false}
	}

	return pgtype.Timestamptz{
		Time:  *t,
		Valid: true,
	}
}

func fromPgTimestamp(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}

	value := t.Time

	return &value
}

func toDomainTask(row db.Task) *domaintask.Task {
	return &domaintask.Task{
		ID:           row.ID,
		PlanDate:     fromPgDate(row.PlanDate),
		RecurrenceID: projectString(row.RecurrenceID),
		ProjectID:    projectString(row.ProjectID),
		OwnerID:      domainaccount.UserID(row.OwnerID.String()),
		Title:        row.Title,
		Description:  row.Description,
		Completed:    row.Completed,
		Priority:     int(row.Priority),
		CreatedAt:    row.CreatedAt,
		DueDate:      fromPgTimestamp(row.DueDate),
		Version:      row.Version,
	}
}

func projectString(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}

	value := id.String()

	return &value
}

func projectUUID(raw *string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if raw == nil {
		return id, nil
	}
	if err := id.Scan(*raw); err != nil || !id.Valid {
		return id, domainproject.ErrInvalid
	}

	return id, nil
}

func toPgDate(raw *string) pgtype.Date {
	if raw == nil {
		return pgtype.Date{}
	}

	value, err := domainplanning.ParseDate(*raw)
	if err != nil {
		return pgtype.Date{}
	}
	// callers validate before conversion
	return pgtype.Date{Time: value, Valid: true}
}

func fromPgDate(value pgtype.Date) *string {
	if !value.Valid {
		return nil
	}

	raw := value.Time.Format(domainplanning.DateLayout)

	return &raw
}
