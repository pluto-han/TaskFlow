package infratask

import (
	"context"
	"errors"
	"fmt"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
)

type Repository struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		queries: db.New(pool),
		pool:    pool,
	}
}

func (r *Repository) FindByID(
	ctx context.Context,
	ownerID domainaccount.UserID,
	id string,
) (*domaintask.Task, error) {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return nil, err
	}

	row, err := r.queries.GetTask(ctx, db.GetTaskParams{ID: id, OwnerID: owner})

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domaintask.ErrTaskNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}

	return toDomainTask(row), nil
}

func (r *Repository) FindPage(
	ctx context.Context, ownerID domainaccount.UserID, query domaintask.PageQuery,
) ([]domaintask.Task, error) {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return nil, err
	}

	if err := domaintask.ValidatePageQuery(query); err != nil {
		return nil, err
	}

	query.ListFilter, _ = domaintask.NormalizeFilter(query.ListFilter)
	params := db.ListTasksParams{
		PlanDateFilter: toPgDate(query.PlanDate),
		OwnerID:        owner,
		PageLimit:      int32(query.Limit),
		DueFrom:        toPgTimestamp(query.DueFrom),
		DueBefore:      toPgTimestamp(query.DueBefore),
	}
	if query.ProjectID != nil {
		if *query.ProjectID == "none" {
			params.Unassigned = true
		} else {
			params.ProjectFilter, _ = projectUUID(query.ProjectID)
		}
	}
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

	tasks := make([]domaintask.Task, 0, len(rows))

	for _, row := range rows {
		tasks = append(tasks, *toDomainTask(row))
	}

	return tasks, nil
}

func (r *Repository) Save(
	ctx context.Context,
	ownerID domainaccount.UserID,
	task *domaintask.Task,
) error {
	if task.RecurrenceID != nil {
		return domaintask.ErrInvalidTask
	} // generated tasks need the occurrence transaction
	if err := domaintask.ValidateTaskPlan(task); err != nil {
		return err
	}

	projectID, err := projectUUID(task.ProjectID)
	if err != nil {
		return err
	}

	owner, err := ownerUUID(ownerID)
	if err != nil {
		return err
	}

	err = r.write(ctx, ownerID, nil, task.ProjectID, func(q *db.Queries, _ *db.Task) error {
		return q.CreateTask(
			ctx,
			db.CreateTaskParams{
				PlanDate:    toPgDate(task.PlanDate),
				ProjectID:   projectID,
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
	})

	if err != nil {
		return mapProjectConstraint(fmt.Errorf("save task: %w", err))
	}

	// CreateTask uses the database default version.
	task.OwnerID = ownerID
	task.Version = 1

	return nil
}

func (r *Repository) Update(
	ctx context.Context,
	ownerID domainaccount.UserID,
	task *domaintask.Task,
) error {
	projectID, err := projectUUID(task.ProjectID)
	if err != nil {
		return err
	}

	owner, err := ownerUUID(ownerID)
	if err != nil {
		return err
	}

	snapshot, err := r.FindByID(ctx, ownerID, task.ID)
	if err != nil {
		return err
	}

	var updated db.Task
	err = r.write(ctx, ownerID, snapshot, task.ProjectID, func(q *db.Queries, current *db.Task) error {
		if !domaintask.SameProject(projectString(current.RecurrenceID), task.RecurrenceID) || !domaintask.SameProject(fromPgDate(current.PlanDate), task.PlanDate) || (current.PlanDate.Valid && task.ProjectID != nil) {
			return domaintask.ErrInvalidUpdate
		}
		if current.Version != task.Version {
			return domaintask.ErrTaskConflict
		}

		row, e := q.UpdateTask(
			ctx,
			db.UpdateTaskParams{
				ProjectID:   projectID,
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

		if errors.Is(e, pgx.ErrNoRows) {
			return domaintask.ErrTaskConflict
		}
		if e != nil {
			return e
		}

		updated = row

		return nil
	})

	if err != nil {
		return mapProjectConstraint(fmt.Errorf("update task: %w", err))
	}

	task.OwnerID = domainaccount.UserID(updated.OwnerID.String())
	task.Version = updated.Version

	return nil
}

func (r *Repository) Delete(ctx context.Context, ownerID domainaccount.UserID, id string) error {
	owner, err := ownerUUID(ownerID)
	if err != nil {
		return err
	}

	snapshot, err := r.FindByID(ctx, ownerID, id)
	if err != nil {
		return err
	}

	err = r.write(ctx, ownerID, snapshot, snapshot.ProjectID, func(q *db.Queries, _ *db.Task) error {
		_, e := q.DeleteTask(ctx, db.DeleteTaskParams{ID: id, OwnerID: owner})

		return e
	})

	if errors.Is(err, pgx.ErrNoRows) {
		return domaintask.ErrTaskNotFound
	}

	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	return nil
}

var _ domaintask.Repository = (*Repository)(nil)

func ownerUUID(id domainaccount.UserID) (pgtype.UUID, error) {
	var result pgtype.UUID
	if id == "" {
		return result, domainaccount.ErrUnauthorized
	}
	if err := result.Scan(string(id)); err != nil || !result.Valid {
		return result, domainaccount.ErrUnauthorized
	}

	return result, nil
}

func mapProjectConstraint(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && (pgErr.ConstraintName == "tasks_project_owner_fk" || pgErr.ConstraintName == "tasks_project_fk") {
		return domainproject.ErrNotFound
	}

	return err
}
