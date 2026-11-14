package infrarecurrence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	db "github.com/pluto-han/taskflow/internal/db/sqlc"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
	domainrecurrence "github.com/pluto-han/taskflow/internal/domain/recurrence"
)

func (r *Repository) GenerateBatch(
	ctx context.Context,
	now func() time.Time,
) (applicationrecurrence.BatchResult, error) {
	// Bound direct callers too, not just Scheduler.Run.
	ctx, cancel := context.WithTimeout(ctx, applicationrecurrence.BatchTimeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return applicationrecurrence.BatchResult{}, err
	}

	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	q := r.q.WithTx(tx)
	day := domainplanning.At(now()).PlanDate
	rows, err := q.LockDueRecurrences(ctx, db.LockDueRecurrencesParams{
		Today:      date(day),
		BatchLimit: applicationrecurrence.BatchSize,
	})
	if err != nil {
		return applicationrecurrence.BatchResult{}, err
	}

	result := applicationrecurrence.BatchResult{}
	// All rows in this batch use the same captured date, so owner deduplication
	// also deduplicates owner/date cache keys.
	affected := make(map[domainaccount.UserID]struct{})
	for _, row := range rows {
		if domainplanning.At(now()).PlanDate != day {
			return applicationrecurrence.BatchResult{}, applicationrecurrence.ErrDayChanged
		}

		anchor := row.AnchorDate.Time.Format(time.DateOnly)
		dates, err := domainrecurrence.Dates(anchor, int(row.IntervalDays), day, 1)
		if err != nil {
			// The finite calendar is exhausted; stop scanning this rule.
			if err = q.AdvanceRecurrence(ctx, db.AdvanceRecurrenceParams{
				ID:          row.ID,
				NextRunDate: row.NextRunDate,
				Paused:      true,
			}); err != nil {
				return applicationrecurrence.BatchResult{}, err
			}

			result.Processed++
			continue
		}
		next := dates[0]
		paused := false
		if next == day {
			inserted, err := q.InsertOccurrence(ctx, db.InsertOccurrenceParams{
				RuleID:        row.ID,
				OwnerID:       row.OwnerID,
				ScheduledDate: date(day),
			})
			if err != nil {
				return applicationrecurrence.BatchResult{}, err
			}
			if inserted == 1 {
				var id [16]byte
				if _, err = rand.Read(id[:]); err != nil {
					return applicationrecurrence.BatchResult{}, err
				}

				created := now()
				if domainplanning.At(created).PlanDate != day {
					return applicationrecurrence.BatchResult{}, applicationrecurrence.ErrDayChanged
				}

				err = q.InsertRecurringTask(ctx, db.InsertRecurringTaskParams{
					ID: hex.EncodeToString(id[:]), OwnerID: row.OwnerID, Title: row.Title, Description: row.Description,
					Priority: row.Priority, PlanDate: date(day), RecurrenceID: row.ID,
					CreatedAt: created.UTC(),
				})
				if err != nil {
					return applicationrecurrence.BatchResult{}, err
				}
				if row.ReminderEnabled {
					var eventID [16]byte
					if _, err = rand.Read(eventID[:]); err != nil {
						return applicationrecurrence.BatchResult{}, err
					}

					eventID[6] = (eventID[6] & 15) | 64
					eventID[8] = (eventID[8] & 63) | 128
					_, err = tx.Exec(ctx, `INSERT INTO notification_outbox(id,owner_id,rule_id,scheduled_date,task_id,title,channel_id,binding_version,next_attempt_at,expires_at)
                      SELECT $1,$2,$3,$4,$5,$6,b.channel_id,b.version,$7,$8 FROM discord_bindings b WHERE b.owner_id=$2 AND b.channel_id IS NOT NULL`,
						eventID, row.OwnerID, row.ID, date(day), hex.EncodeToString(id[:]), row.Title, created, domainplanning.At(created).NextDayAt)
					if err != nil {
						return applicationrecurrence.BatchResult{}, err
					}
				}
				result.Generated++
				affected[domainaccount.UserID(row.OwnerID.String())] = struct{}{}
			}
			next, err = domainrecurrence.NextAfter(day, anchor, int(row.IntervalDays), day)
			if err != nil {
				paused = true
				next = day
			}
		}
		if err = q.AdvanceRecurrence(ctx, db.AdvanceRecurrenceParams{
			ID:          row.ID,
			NextRunDate: date(next),
			Paused:      paused,
		}); err != nil {
			return applicationrecurrence.BatchResult{}, err
		}

		result.Processed++
	}
	// Recheck immediately before commit; detected date changes roll back the batch.
	if domainplanning.At(now()).PlanDate != day {
		return applicationrecurrence.BatchResult{}, applicationrecurrence.ErrDayChanged
	}
	if err = tx.Commit(ctx); err != nil {
		return applicationrecurrence.BatchResult{}, err
	}

	for owner := range affected {
		result.Affected = append(result.Affected, applicationrecurrence.AffectedPlan{
			Owner: owner,
			Date:  day,
		})
	}

	return result, nil
}
