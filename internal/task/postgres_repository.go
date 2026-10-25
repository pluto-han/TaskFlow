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
		Title:       row.Title,
		Description: row.Description,
		Completed:   row.Completed,
		Priority:    int(row.Priority),
		CreatedAt:   row.CreatedAt,
		DueDate:     fromPgTimestamp(row.DueDate),
	}
}

func (r *PostgresRepository) FindByID(ctx context.Context, id string) (*Task, error) {

	row, err := r.queries.GetTask(ctx, id)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaskNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}

	return &Task{
		ID:          row.ID,
		Title:       row.Title,
		Description: row.Description,
		Completed:   row.Completed,
		Priority:    int(row.Priority),
		CreatedAt:   row.CreatedAt,
		DueDate:     fromPgTimestamp(row.DueDate),
	}, nil
}

func (r *PostgresRepository) FindAll(
	ctx context.Context,
) ([]Task, error) {

	rows, err := r.queries.ListTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	tasks := make([]Task, 0, len(rows))

	for _, row := range rows {
		tasks = append(tasks, *toDomainTask(row))
	}

	return tasks, nil
}

func (r *PostgresRepository) Save(ctx context.Context, task *Task) error {

	err := r.queries.CreateTask(
		ctx,
		db.CreateTaskParams{
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

	return nil
}

func (r *PostgresRepository) Update(ctx context.Context, task *Task) error {
	_, err := r.queries.UpdateTask(
		ctx,
		db.UpdateTaskParams{
			ID:          task.ID,
			Title:       task.Title,
			Description: task.Description,
			Completed:   task.Completed,
			Priority:    int32(task.Priority),
			DueDate:     toPgTimestamp(task.DueDate),
		},
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTaskNotFound
	}

	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	return nil
}

func (r *PostgresRepository) Delete(ctx context.Context, id string) error {
	_, err := r.queries.DeleteTask(ctx, id)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTaskNotFound
	}

	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	return nil
}

var _ Repository = (*PostgresRepository)(nil)
