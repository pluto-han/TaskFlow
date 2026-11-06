package recurrence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	"github.com/pluto-han/taskflow/internal/planning"
)

// WorkerPoolConfig isolates background work from HTTP pool acquisitions.
// Each API process uses at most one additional PostgreSQL connection.
func WorkerPoolConfig(url string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.MinIdleConns = 0
	cfg.ConnConfig.RuntimeParams["application_name"] = "taskflow-recurrence"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "4000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "1000"
	return cfg, nil
}
func (r *PostgresRepository) GenerateBatch(ctx context.Context, now func() time.Time) (BatchResult, error) {
	// Bound direct callers too, not just Scheduler.Run.
	ctx, cancel := context.WithTimeout(ctx, BatchTimeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return BatchResult{}, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	q := r.q.WithTx(tx)
	day := planning.At(now()).PlanDate
	rows, err := q.LockDueRecurrences(ctx, db.LockDueRecurrencesParams{Today: date(day), BatchLimit: BatchSize})
	if err != nil {
		return BatchResult{}, err
	}
	result := BatchResult{}
	for _, row := range rows {
		if planning.At(now()).PlanDate != day {
			return BatchResult{}, ErrDayChanged
		}
		anchor := row.AnchorDate.Time.Format(time.DateOnly)
		dates, err := Dates(anchor, int(row.IntervalDays), day, 1)
		if err != nil {
			// The finite calendar is exhausted; stop scanning this rule.
			if err = q.AdvanceRecurrence(ctx, db.AdvanceRecurrenceParams{ID: row.ID, NextRunDate: row.NextRunDate, Paused: true}); err != nil {
				return BatchResult{}, err
			}
			result.Processed++
			continue
		}
		next := dates[0]
		paused := false
		if next == day {
			inserted, err := q.InsertOccurrence(ctx, db.InsertOccurrenceParams{RuleID: row.ID, OwnerID: row.OwnerID, ScheduledDate: date(day)})
			if err != nil {
				return BatchResult{}, err
			}
			if inserted == 1 {
				var id [16]byte
				if _, err = rand.Read(id[:]); err != nil {
					return BatchResult{}, err
				}
				created := now()
				if planning.At(created).PlanDate != day {
					return BatchResult{}, ErrDayChanged
				}
				err = q.InsertRecurringTask(ctx, db.InsertRecurringTaskParams{
					ID: hex.EncodeToString(id[:]), OwnerID: row.OwnerID, Title: row.Title, Description: row.Description,
					Priority: row.Priority, PlanDate: date(day), RecurrenceID: row.ID,
					CreatedAt: created.UTC(),
				})
				if err != nil {
					return BatchResult{}, err
				}
				if row.ReminderEnabled {
					var eventID [16]byte
					if _, err = rand.Read(eventID[:]); err != nil {
						return BatchResult{}, err
					}
					eventID[6] = (eventID[6] & 15) | 64
					eventID[8] = (eventID[8] & 63) | 128
					_, err = tx.Exec(ctx, `INSERT INTO notification_outbox(id,owner_id,rule_id,scheduled_date,task_id,title,channel_id,binding_version,next_attempt_at,expires_at)
                      SELECT $1,$2,$3,$4,$5,$6,b.channel_id,b.version,$7,$8 FROM discord_bindings b WHERE b.owner_id=$2 AND b.channel_id IS NOT NULL`,
						eventID, row.OwnerID, row.ID, date(day), hex.EncodeToString(id[:]), row.Title, created, planning.At(created).NextDayAt)
					if err != nil {
						return BatchResult{}, err
					}
				}
				result.Generated++
			}
			next, err = nextAfter(day, anchor, int(row.IntervalDays), day)
			if err != nil {
				paused = true
				next = day
			}
		}
		if err = q.AdvanceRecurrence(ctx, db.AdvanceRecurrenceParams{ID: row.ID, NextRunDate: date(next), Paused: paused}); err != nil {
			return BatchResult{}, err
		}
		result.Processed++
	}
	// Recheck immediately before commit; detected date changes roll back the batch.
	if planning.At(now()).PlanDate != day {
		return BatchResult{}, ErrDayChanged
	}
	if err = tx.Commit(ctx); err != nil {
		return BatchResult{}, err
	}
	return result, nil
}
