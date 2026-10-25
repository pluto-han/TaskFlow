package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{
		db: db,
	}
}

func (r *PostgresRepository) FindByID(ctx context.Context, id string) (*Task, error) {
	const query = `
		SELECT
			id,
			title,
			description,
			completed,
			priority,
			created_at,
			due_date
		FROM tasks
		WHERE id = $1
	`

	var task Task

	err := r.db.QueryRow(ctx, query, id).Scan(
		&task.ID,
		&task.Title,
		&task.Description,
		&task.Completed,
		&task.Priority,
		&task.CreatedAt,
		&task.DueDate,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTaskNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("find task by id: %w", err)
	}

	return &task, nil
}

func (r *PostgresRepository) FindAll(ctx context.Context) ([]Task, error) {

	const query = `
        SELECT
            id,
            title,
            description,
            completed,
            priority,
            created_at,
            due_date
        FROM tasks
        ORDER BY created_at DESC, id DESC
    `

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)

	for rows.Next() {
		var task Task

		err := rows.Scan(
			&task.ID,
			&task.Title,
			&task.Description,
			&task.Completed,
			&task.Priority,
			&task.CreatedAt,
			&task.DueDate,
		)

		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}

		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}

	return tasks, nil
}

func (r *PostgresRepository) Save(ctx context.Context, task *Task) error {

	const query = `
        INSERT INTO tasks (
            id,
            title,
            description,
            completed,
            priority,
            created_at
        )
        VALUES ($1, $2, $3, $4, $5, $6)
    `

	_, err := r.db.Exec(
		ctx,
		query,
		task.ID,
		task.Title,
		task.Description,
		task.Completed,
		task.Priority,
		task.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("save task: %w", err)
	}

	return nil
}

func (r *PostgresRepository) Update(ctx context.Context, task *Task) error {

	const query = `
        UPDATE tasks
        SET
            title = $2,
            description = $3,
            completed = $4,
            priority = $5
        WHERE id = $1
    `

	result, err := r.db.Exec(
		ctx,
		query,
		task.ID,
		task.Title,
		task.Description,
		task.Completed,
		task.Priority,
	)

	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrTaskNotFound
	}

	return nil
}

func (r *PostgresRepository) Delete(ctx context.Context, id string) error {

	const query = `
        DELETE FROM tasks
        WHERE id = $1
    `

	result, err := r.db.Exec(ctx, query, id)

	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrTaskNotFound
	}

	return nil
}

var _ Repository = (*PostgresRepository)(nil)
