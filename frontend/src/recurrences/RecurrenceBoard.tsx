import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "../auth/AuthProvider";
import { errorMessage } from "../api/client";
import { recurrenceApi, type Recurrence } from "../api/recurrences";
import { RecurrenceEditor } from "./RecurrenceEditor";
import { Modal } from "../components/Modal";

export function RecurrenceBoard() {
  const { authorize } = useAuth();
  const [items, setItems] = useState<Recurrence[]>([]);
  const [schedulerEnabled, setSchedulerEnabled] = useState(false);
  const [day, setDay] = useState("");
  const [cursor, setCursor] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState<Recurrence | "new" | null>(null);
  const [deleting, setDeleting] = useState<Recurrence | null>(null);
  const request = useRef<AbortController | null>(null);
  const load = useCallback(
    async (after = "") => {
      request.current?.abort();
      const controller = new AbortController();
      request.current = controller;
      setBusy(true);
      try {
        const page = await authorize((credentials) =>
          recurrenceApi.list(credentials, after, controller.signal),
        );
        if (controller.signal.aborted) return;
        setItems((old) => (after ? [...old, ...page.items] : page.items));
        setDay(page.plan_date);
        setSchedulerEnabled(page.scheduler_enabled);
        setCursor(page.next_cursor);
        setError("");
      } catch (err) {
        if (!controller.signal.aborted) setError(errorMessage(err));
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    },
    [authorize],
  );
  useEffect(() => {
    // Synchronize the collection with the server.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
    return () => request.current?.abort();
  }, [load]);
  async function mutate(action: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await action();
      setDeleting(null);
      await load();
    } catch (err) {
      setError(
        errorMessage(err) +
          " If the schedule has changed, close this dialog and refresh before trying again.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section aria-label="Recurring tasks">
      <div className="board-heading">
        <div>
          <h1>Recurring tasks</h1>
        </div>
        <button
          className="button primary"
          disabled={busy || !day}
          onClick={() => setEditor("new")}
        >
          New recurring task
        </button>
      </div>
      <p className="hint">
        Automatic creation {schedulerEnabled ? "enabled" : "disabled"}
      </p>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      <button
        className="text-button"
        disabled={busy}
        onClick={() => void load()}
      >
        Refresh schedules
      </button>
      {busy && <p role="status">Syncing…</p>}
      {!busy && !error && !items.length && (
        <p className="empty-state">No recurring tasks</p>
      )}
      <ul className="task-list" aria-busy={busy}>
        {items.map((rule) => (
          <li className="task-row" key={rule.id}>
            <div className="task-content">
              <h2>{rule.title}</h2>
              {rule.description && (
                <p className="task-description">{rule.description}</p>
              )}
              <div className="task-meta">
                <span className={`priority priority-${rule.priority}`}>
                  {
                    ["", "Low priority", "Medium priority", "High priority"][
                      rule.priority
                    ]
                  }
                </span>
                <span>
                  Every {rule.interval_days}{" "}
                  {rule.interval_days === 1 ? "day" : "days"} ·{" "}
                  {rule.paused ? "Paused" : "Active"}
                </span>
                {rule.reminder_enabled && <span>Discord reminder</span>}
              </div>
              <p className="hint">
                {rule.paused
                  ? "Paused"
                  : `Upcoming dates: ${rule.preview_dates.join(", ")} (Amsterdam)`}
              </p>
            </div>
            <div className="row-actions">
              <button
                className="text-button"
                disabled={busy}
                onClick={() => setEditor(rule)}
              >
                Edit
              </button>
              <button
                className="text-button"
                disabled={busy}
                onClick={() =>
                  void mutate(() =>
                    authorize((credentials) =>
                      recurrenceApi.update(credentials, rule.id, {
                        version: rule.version,
                        paused: !rule.paused,
                      }),
                    ),
                  )
                }
              >
                {rule.paused ? "Resume" : "Pause"}
              </button>
              <button
                className="text-button danger-text"
                disabled={busy}
                onClick={() => setDeleting(rule)}
              >
                Delete
              </button>
            </div>
          </li>
        ))}
      </ul>
      {cursor && (
        <div className="load-more">
          <button
            className="button secondary"
            disabled={busy}
            onClick={() => void load(cursor)}
          >
            Load more
          </button>
        </div>
      )}
      {editor && (
        <RecurrenceEditor
          rule={editor === "new" ? undefined : editor}
          day={day}
          close={() => setEditor(null)}
          saved={() => {
            setEditor(null);
            void load();
          }}
        />
      )}
      {deleting && (
        <Modal
          title="Delete recurring task"
          busy={busy}
          close={() => setDeleting(null)}
        >
          {error && (
            <div className="notice error" role="alert">
              {error}
            </div>
          )}
          <p>
            Delete the schedule for “{deleting.title}”? Your existing tasks and
            history will stay.
          </p>
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setDeleting(null)}
            >
              Cancel
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() =>
                void mutate(() =>
                  authorize((credentials) =>
                    recurrenceApi.remove(credentials, deleting),
                  ),
                )
              }
            >
              Confirm delete
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}
