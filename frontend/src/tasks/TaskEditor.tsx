import { useState, type FormEvent } from "react";
import { Modal } from "../components/Modal";
import { useAuth } from "../auth/AuthProvider";
import { workspaceApi } from "../api/workspace";
import { ApiError, errorMessage } from "../api/client";
import type { Task, Project, UpdateTask } from "../api/types";
import { toLocalInput, toUTC, displayDate } from "./dates";

export function TaskEditor({
  todayDate,
  refreshToday,
  task,
  projects,
  defaultProject,
  close,
  saved,
}: {
  todayDate?: string;
  refreshToday?: () => Promise<string | null>;
  task?: Task;
  projects: Project[];
  defaultProject: string;
  close(): void;
  saved(): void;
}) {
  const { authorize } = useAuth();
  const [draftDay, setDraftDay] = useState(todayDate);
  const [dayConflict, setDayConflict] = useState(false);
  const [base, setBase] = useState(task);
  const [title, setTitle] = useState(task?.title || "");
  const [description, setDescription] = useState(task?.description || "");
  const [priority, setPriority] = useState(task?.priority || 2);
  const [project, setProject] = useState(
    task?.project_id || (defaultProject === "none" ? "" : defaultProject),
  );
  const [due, setDue] = useState(toLocalInput(task?.due_date || null));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [latest, setLatest] = useState<Task | null>(null);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (
      busy ||
      conflict ||
      dayConflict ||
      (!base && !!draftDay && draftDay !== todayDate)
    )
      return;
    if (!title.trim()) {
      setError("Give your task a title.");
      return;
    }
    let dueDate: string | null;
    try {
      dueDate = toUTC(due);
    } catch (err) {
      setError((err as Error).message);
      return;
    }
    setBusy(true);
    setError("");
    try {
      if (!base && draftDay) {
        await authorize((credentials) =>
          workspaceApi.createToday(credentials, {
            plan_date: draftDay,
            title: title.trim(),
            description,
            priority,
            due_date: dueDate,
          }),
        );
      } else if (!base)
        await authorize((credentials) =>
          workspaceApi.createTask(credentials, {
            title: title.trim(),
            description,
            priority,
            project_id: project || null,
            due_date: dueDate,
          }),
        );
      else {
        const patch: UpdateTask = { version: base.version };
        if (title.trim() !== base.title) patch.title = title.trim();
        if (description !== base.description) patch.description = description;
        if (priority !== base.priority) patch.priority = priority;
        if ((project || null) !== base.project_id)
          patch.project_id = project || null;
        // Preserve sub-second precision and DST-fold identity when not edited.
        if (due !== toLocalInput(base.due_date)) patch.due_date = dueDate;
        if (Object.keys(patch).length === 1) {
          close();
          return;
        }
        await authorize((credentials) =>
          workspaceApi.updateTask(credentials, base.id, patch),
        );
      }
      saved();
    } catch (err) {
      if (err instanceof ApiError && err.code === "PLAN_DAY_CHANGED")
        setDayConflict(true);
      else if (err instanceof ApiError && err.status === 409) setConflict(true);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  async function fetchLatest() {
    if (!base) return;
    setBusy(true);
    setError("");
    try {
      setLatest(
        await authorize((credentials) =>
          workspaceApi.task(credentials, base.id),
        ),
      );
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  function useLatest() {
    if (!latest) return;
    setBase(latest);
    setTitle(latest.title);
    setDescription(latest.description);
    setPriority(latest.priority);
    setProject(latest.project_id || "");
    setDue(toLocalInput(latest.due_date));
    setConflict(false);
    setLatest(null);
    setError("");
  }
  return (
    <Modal title={base ? "Edit task" : "New task"} busy={busy} close={close}>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      {!base && draftDay && (dayConflict || draftDay !== todayDate) && (
        <section className="notice" aria-label="Planning date changed">
          <p>
            A new day has begun in Amsterdam. Your draft is safe. Confirm the
            new date before saving; nothing will be submitted automatically.
          </p>
          <button
            type="button"
            className="button secondary"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                const date = await refreshToday?.();
                if (date) {
                  setDraftDay(date);
                  setDayConflict(false);
                  setError("");
                } else
                  setError(
                    "We could not confirm today’s date. Please try again; your draft is safe.",
                  );
              } catch (err) {
                setError(errorMessage(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            Refresh and confirm date
          </button>
        </section>
      )}
      {(base?.plan_date || draftDay) && (
        <p className="hint">
          Planned for: {base?.plan_date || draftDay} · Europe/Amsterdam.
          Personal plans stay outside projects.
        </p>
      )}
      {conflict && (
        <section className="notice" aria-label="Version conflict">
          <p>
            Your draft is safe. Load the latest version, review the changes,
            then edit again. Other changes will not be overwritten
            automatically.
          </p>
          <button
            className="button secondary"
            disabled={busy}
            type="button"
            onClick={() => void fetchLatest()}
          >
            Load latest version
          </button>
          {latest && (
            <div className="comparison">
              <strong>Latest saved version</strong>
              <p>{latest.title}</p>
              <p className="preserve-lines">
                {latest.description || "No description"}
              </p>
              <p>
                Priority {latest.priority} ·{" "}
                {latest.completed ? "Completed" : "Incomplete"} ·{" "}
                {latest.due_date ? displayDate(latest.due_date) : "No due date"}
              </p>
              <p>
                Project:{" "}
                {projects.find((p) => p.id === latest.project_id)?.name ||
                  "Unassigned"}
              </p>
              <button
                type="button"
                className="button secondary"
                disabled={busy}
                onClick={useLatest}
              >
                Discard draft and use latest version
              </button>
            </div>
          )}
        </section>
      )}
      {project && (
        <p className="notice">
          Everyone in this project can view, edit, and delete its tasks. Only
          the person who created a task can move it to another project or remove
          it from this one.
        </p>
      )}
      {base && !base.can_move && !base.plan_date && (
        <p className="hint">
          Only the person who created this task can change its project.
        </p>
      )}
      <form onSubmit={(event) => void submit(event)} aria-busy={busy}>
        <div className="field">
          <label htmlFor="task-title">Task title</label>
          <input
            id="task-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            disabled={busy}
          />
        </div>
        <div className="field">
          <label htmlFor="task-description">Description</label>
          <textarea
            id="task-description"
            rows={3}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            disabled={busy}
          />
        </div>
        <div className="form-grid">
          <div className="field">
            <label htmlFor="task-priority">Priority</label>
            <select
              id="task-priority"
              value={priority}
              onChange={(e) => setPriority(Number(e.target.value))}
              disabled={busy}
            >
              <option value={1}>Low</option>
              <option value={2}>Medium</option>
              <option value={3}>High</option>
            </select>
          </div>
          {!draftDay && !base?.plan_date && (
            <div className="field">
              <label htmlFor="task-project">Project</label>
              <select
                id="task-project"
                value={project}
                onChange={(e) => setProject(e.target.value)}
                disabled={busy || (!!base && !base.can_move)}
              >
                <option value="">Unassigned</option>
                {project && !projects.some((p) => p.id === project) && (
                  <option value={project}>
                    Current project (refresh your projects)
                  </option>
                )}
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
        <div className="field">
          <label htmlFor="task-due">Due date (local time)</label>
          <input
            id="task-due"
            type="datetime-local"
            step="1"
            value={due}
            onChange={(e) => setDue(e.target.value)}
            disabled={busy}
          />
          <p className="hint">
            Your time zone: {Intl.DateTimeFormat().resolvedOptions().timeZone}.
            Leave blank for no due date. When daylight saving repeats a time,
            your browser uses the earlier occurrence.
          </p>
          {due && (
            <button
              type="button"
              className="text-button"
              disabled={busy}
              onClick={() => setDue("")}
            >
              Clear due date
            </button>
          )}
        </div>
        <div className="modal-actions">
          <button
            type="button"
            className="button secondary"
            disabled={busy}
            onClick={close}
          >
            Cancel
          </button>
          <button
            type="submit"
            className="button primary"
            disabled={
              busy ||
              conflict ||
              dayConflict ||
              (!base && !!draftDay && draftDay !== todayDate)
            }
          >
            {busy ? "Saving…" : "Save task"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
