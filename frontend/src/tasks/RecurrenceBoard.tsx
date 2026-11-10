import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useAuth } from "../auth/AuthProvider";
import { errorMessage } from "../api/client";
import { recurrenceApi, type Recurrence } from "../api/recurrences";
import { discordApi } from "../api/discord";
import { Link } from "react-router";
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
          <span className="eyebrow">At your own pace</span>
          <h1>Recurring tasks</h1>
          <p className="subtitle">
            Repeats by the Amsterdam calendar. Finishing a task does not change
            its rhythm.
          </p>
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
        {schedulerEnabled
          ? "Automatic tasks are on. Tasks join Today by the Amsterdam calendar, usually within about 30 seconds."
          : "Schedules and date previews are saved, but automatic task creation is not enabled yet."}
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
        <div className="empty-state">
          <span aria-hidden="true">○</span>
          <h2>Room for a new rhythm</h2>
          <p>Begin with something small, every day or every few days.</p>
        </div>
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
                  ? "Resuming starts from the present; missed dates stay in the past."
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

export function RecurrenceEditor({
  rule,
  day,
  close,
  saved,
}: {
  rule?: Recurrence;
  day: string;
  close(): void;
  saved(): void;
}) {
  const { authorize } = useAuth();
  const [title, setTitle] = useState(rule?.title ?? "");
  const [reminderEnabled, setReminderEnabled] = useState(
    rule?.reminder_enabled ?? false,
  );
  const [discordBound, setDiscordBound] = useState<boolean | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void authorize((credentials) =>
      discordApi.status(credentials, controller.signal),
    )
      .then((result) => {
        if (!controller.signal.aborted) setDiscordBound(result.bound);
      })
      .catch(() => {
        if (!controller.signal.aborted) setDiscordBound(false);
      });
    return () => controller.abort();
  }, [authorize]);
  const [description, setDescription] = useState(rule?.description ?? "");
  const [priority, setPriority] = useState(rule?.priority ?? 2);
  const [anchor, setAnchor] = useState(rule?.anchor_date ?? day);
  const [interval, setInterval] = useState(rule?.interval_days ?? 1);
  const [dates, setDates] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [previewError, setPreviewError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(() => {
      setDates([]);
      void authorize((credentials) =>
        recurrenceApi.preview(credentials, anchor, interval, controller.signal),
      )
        .then((result) => {
          if (!controller.signal.aborted) {
            setDates(result.dates);
            setPreviewError("");
          }
        })
        .catch((err) => {
          if (!controller.signal.aborted) setPreviewError(errorMessage(err));
        });
    }, 250);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [anchor, interval, authorize]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    if (
      rule &&
      (interval !== rule.interval_days || anchor !== rule.anchor_date) &&
      anchor <= day
    ) {
      setError(
        "Choose a date after today for the new schedule to take effect.",
      );
      return;
    }
    setBusy(true);
    setError("");
    try {
      const body = {
        title: title.trim(),
        description,
        priority,
        interval_days: interval,
        reminder_enabled: reminderEnabled,
      };
      if (rule)
        await authorize((credentials) =>
          recurrenceApi.update(credentials, rule.id, {
            ...body,
            version: rule.version,
            ...(anchor !== rule.anchor_date || interval !== rule.interval_days
              ? { anchor_date: anchor }
              : {}),
          }),
        );
      else
        await authorize((credentials) =>
          recurrenceApi.create(credentials, { ...body, anchor_date: anchor }),
        );
      saved();
    } catch (err) {
      setError(
        errorMessage(err) +
          " Your draft is safe. If the schedule has changed, cancel and refresh it before editing again.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={rule ? "Edit recurring task" : "New recurring task"}
      busy={busy}
      close={close}
    >
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      <form onSubmit={(event) => void submit(event)} aria-busy={busy}>
        <div className="field">
          <label htmlFor="rec-title">Task title</label>
          <input
            id="rec-title"
            value={title}
            maxLength={200}
            required
            disabled={busy}
            onChange={(e) => setTitle(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="rec-description">Description</label>
          <textarea
            id="rec-description"
            rows={3}
            maxLength={2000}
            value={description}
            disabled={busy}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <div className="form-grid">
          <div className="field">
            <label htmlFor="rec-priority">Priority</label>
            <select
              id="rec-priority"
              value={priority}
              disabled={busy}
              onChange={(e) => setPriority(Number(e.target.value))}
            >
              <option value={1}>Low</option>
              <option value={2}>Medium</option>
              <option value={3}>High</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="rec-interval">Repeat every (days)</label>
            <input
              id="rec-interval"
              type="number"
              min={1}
              max={365}
              required
              value={interval}
              disabled={busy}
              onChange={(e) => {
                setInterval(Number(e.target.value));
                setDates([]);
              }}
            />
          </div>
        </div>
        <div className="field">
          <label htmlFor="rec-anchor">
            {rule ? "Start date (changes apply to future tasks)" : "First date"}
          </label>
          <input
            id="rec-anchor"
            type="date"
            min={rule ? undefined : day}
            max="9999-12-31"
            value={anchor}
            required
            disabled={busy}
            onChange={(e) => {
              setAnchor(e.target.value);
              setDates([]);
            }}
          />
        </div>
        <div className="field reminder-option">
          <label htmlFor="rec-reminder">
            <input
              id="rec-reminder"
              type="checkbox"
              checked={reminderEnabled}
              disabled={busy || (!discordBound && !reminderEnabled)}
              onChange={(e) => setReminderEnabled(e.target.checked)}
            />{" "}
            Discord channel reminder
          </label>
          {!discordBound && (
            <p className="hint">
              First, connect a Discord channel in your{" "}
              <Link to="/profile">Profile</Link>.
            </p>
          )}
        </div>
        <p className="hint">
          Dates follow the Europe/Amsterdam calendar. Schedule changes must
          start in the future. Existing tasks stay as they are, and missed dates
          are not backfilled.
        </p>
        <p className="hint" aria-live="polite">
          {previewError ||
            (dates.length
              ? `Schedule preview: ${dates.join(", ")}`
              : "Finding the next dates…")}
        </p>
        <div className="modal-actions">
          <button
            type="button"
            className="button secondary"
            disabled={busy}
            onClick={close}
          >
            Cancel
          </button>
          <button type="submit" className="button primary" disabled={busy}>
            {busy ? "Saving…" : "Save schedule"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
