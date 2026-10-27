package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	"github.com/pluto-han/taskflow/internal/session"
)

type PostgresRepository struct {
	queries *db.Queries
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		queries: db.New(pool),
	}
}

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

func toDomainTask(row db.Task) *Task {
	return &Task{
		ID:          row.ID,
		OwnerID:     session.UserID(row.OwnerID.String()),
		Title:       row.Title,
		Description: row.Description,
		Completed:   row.Completed,
		Priority:    int(row.Priority),
		CreatedAt:   row.CreatedAt,
		DueDate:     fromPgTimestamp(row.DueDate),
		Version:     row.Version,
	}
}

func (r *PostgresRepository) FindByID(ctx context.Context, ownerID session.UserID, id string) (*Task, error) {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return nil, err
	}

	row, err := r.queries.GetTask(ctx, db.GetTaskParams{ID: id, OwnerID: owner})

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaskNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}

	return toDomainTask(row), nil
}

func (r *PostgresRepository) FindPage(
	ctx context.Context, ownerID session.UserID, query PageQuery,
) ([]Task, error) {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return nil, err
	}

	if err := validatePageQuery(query); err != nil {
		return nil, err
	}
	params := db.ListTasksParams{OwnerID: owner, PageLimit: int32(query.Limit), DueFrom: toPgTimestamp(query.DueFrom), DueBefore: toPgTimestamp(query.DueBefore)}
	if query.Completed != nil {
		params.CompletedFilter = pgtype.Bool{Bool: *query.Completed, Valid: true}
	}
	if query.Priority != nil {
		params.PriorityFilter = pgtype.Int4{Int32: int32(*query.Priority), Valid: true}
	}
	if query.HasDueDate != nil {
		params.HasDueDate = pgtype.Bool{Bool: *query.HasDueDate, Valid: true}
	}
	if query.After != nil {
		params.AfterCreatedAt = toPgTimestamp(&query.After.CreatedAt)
		params.AfterID = query.After.ID
	}
	rows, err := r.queries.ListTasks(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	tasks := make([]Task, 0, len(rows))

	for _, row := range rows {
		tasks = append(tasks, *toDomainTask(row))
	}

	return tasks, nil
}

func (r *PostgresRepository) Save(ctx context.Context, ownerID session.UserID, task *Task) error {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return err
	}

	err = r.queries.CreateTask(
		ctx,
		db.CreateTaskParams{
			OwnerID:     owner,
			ID:          task.ID,
			Title:       task.Title,
			Description: task.Description,
			Completed:   task.Completed,
			Priority:    int32(task.Priority),
			CreatedAt:   task.CreatedAt,
			DueDate:     toPgTimestamp(task.DueDate),
		},
	)

	if err != nil {
		return fmt.Errorf("save task: %w", err)
	}

	// CreateTask uses the database default version.
	task.OwnerID = ownerID
	task.Version = 1
	return nil
}

func (r *PostgresRepository) Update(ctx context.Context, ownerID session.UserID, task *Task) error {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return err
	}

	row, err := r.queries.UpdateTask(
		ctx,
		db.UpdateTaskParams{
			OwnerID:     owner,
			ID:          task.ID,
			Title:       task.Title,
			Description: task.Description,
			Completed:   task.Completed,
			Priority:    int32(task.Priority),
			DueDate:     toPgTimestamp(task.DueDate),
			Version:     task.Version,
		},
	)

	if errors.Is(err, pgx.ErrNoRows) {
		_, findErr := r.queries.GetTask(ctx, db.GetTaskParams{ID: task.ID, OwnerID: owner})

		if errors.Is(findErr, pgx.ErrNoRows) {
			return ErrTaskNotFound
		}

		if findErr != nil {
			return fmt.Errorf(
				"check task after failed update: %w",
				findErr,
			)
		}

		return ErrTaskConflict
	}

	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	task.OwnerID = ownerID
	task.Version = row.Version

	return nil
}

func (r *PostgresRepository) Delete(ctx context.Context, ownerID session.UserID, id string) error {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return err
	}

	_, err = r.queries.DeleteTask(ctx, db.DeleteTaskParams{ID: id, OwnerID: owner})

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTaskNotFound
	}

	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	return nil
}

var _ Repository = (*PostgresRepository)(nil)

func ownerUUID(id session.UserID) (pgtype.UUID, error) {
	var result pgtype.UUID
	if id == "" {
		return result, session.ErrUnauthorized
	}
	if err := result.Scan(string(id)); err != nil || !result.Valid {
		return result, session.ErrUnauthorized
	}
	return result, nil
}
