import { Link } from "react-router";
import { useEffect, useState, type FormEvent } from "react";
import { useAuth } from "../auth/AuthProvider";
import { errorMessage } from "../api/client";
import { recurrenceApi, type Recurrence } from "../api/recurrences";
import { discordApi } from "../api/discord";

import { Modal } from "../components/Modal";

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
              <Link to="/settings/integrations">Connect Discord</Link>
            </p>
          )}
        </div>

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
