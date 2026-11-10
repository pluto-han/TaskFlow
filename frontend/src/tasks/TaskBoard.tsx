import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "../auth/AuthProvider";
import { workspaceApi, type Filters } from "../api/workspace";
import { errorMessage, ApiError } from "../api/client";
import type { Task, Project, TodayPage } from "../api/types";
import { TaskEditor } from "./TaskEditor";
import { DeleteDialog } from "./DeleteDialog";
import { displayDate } from "./dates";

export function TaskBoard({
  today = false,
  filters,
  projects,
  title,
  setMutating,
}: {
  today?: boolean;
  filters: Filters;
  projects: Project[];
  title: string;
  setMutating(busy: boolean): void;
}) {
  const { authorize } = useAuth();
  const [day, setDay] = useState<TodayPage | null>(null);
  const dayRef = useRef("");
  const midnightTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const [items, setItems] = useState<Task[]>([]);
  const [now, setNow] = useState(() => Date.now());
  const [cursor, setCursor] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [pending, setPending] = useState("");
  const [editor, setEditor] = useState<Task | "new" | null>(null);
  const [deleting, setDeleting] = useState<Task | null>(null);
  const controller = useRef<AbortController | null>(null);
  const locked = useRef(false);
  const active = useRef(false);
  const load = useCallback(
    async function fetchPage(after = ""): Promise<string | null> {
      if (!active.current) return null;
      controller.current?.abort();
      const request = new AbortController();
      controller.current = request;
      const started = performance.now();
      if (today && !after) {
        clearTimeout(midnightTimer.current);
        setItems([]);
        setCursor("");
      }
      setLoading(true);
      setError("");
      try {
        const response = await authorize<
          TodayPage | { items: Task[]; next: string }
        >((credentials) =>
          today
            ? workspaceApi.today(credentials, filters, after, request.signal)
            : workspaceApi.tasks(credentials, filters, after, request.signal),
        );
        if (request.signal.aborted) return null;
        let result: { items: Task[]; next: string };
        if ("plan_date" in response) {
          // Use server duration, not the browser's wall clock or timezone.
          const remaining =
            Date.parse(response.next_day_at) -
            Date.parse(response.server_time) -
            (performance.now() - started);
          if (!Number.isFinite(remaining))
            throw new ApiError("INVALID_RESPONSE");
          clearTimeout(midnightTimer.current);
          if (remaining <= 0) {
            setItems([]);
            setCursor("");
            setDay(null);
            midnightTimer.current = setTimeout(() => void fetchPage(), 1000);
            return null;
          }
          if (after && dayRef.current !== response.plan_date)
            return await fetchPage();
          dayRef.current = response.plan_date;
          setDay(response);
          midnightTimer.current = setTimeout(
            () => void fetchPage(),
            Math.min(remaining, 2147483647),
          );
          result = { items: response.items, next: response.next_cursor };
        } else result = response;
        setItems((previous) =>
          after
            ? [
                ...previous,
                ...result.items.filter(
                  (item) => !previous.some((p) => p.id === item.id),
                ),
              ]
            : result.items,
        );
        setNow(Date.now());
        setCursor(result.next);
        setConflict(false);
        return "plan_date" in response ? response.plan_date : null;
      } catch (err) {
        if (!request.signal.aborted) {
          if (
            today &&
            after &&
            err instanceof ApiError &&
            err.code === "INVALID_QUERY"
          )
            return await fetchPage();
          setError(errorMessage(err));
        }
        return null;
      } finally {
        if (!request.signal.aborted) setLoading(false);
      }
    },
    [authorize, filters, today],
  );
  useEffect(() => {
    // Fetch state belongs to this keyed filter view; cleanup rejects stale pages.
    // Explicit fetch boundary; load also supports user-triggered refresh.
    active.current = true;
    void load();
    const refresh = () => void load();
    const visible = () => {
      if (document.visibilityState === "visible") refresh();
    };
    if (today) {
      window.addEventListener("focus", refresh);
      window.addEventListener("online", refresh);
      document.addEventListener("visibilitychange", visible);
    }
    return () => {
      active.current = false;
      controller.current?.abort();
      clearTimeout(midnightTimer.current);
      window.removeEventListener("focus", refresh);
      window.removeEventListener("online", refresh);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [load, today]);

  async function toggle(item: Task) {
    if (locked.current || loading) return;
    locked.current = true;
    setPending(item.id);
    setMutating(true);
    setError("");
    setItems((previous) =>
      previous.map((row) =>
        row.id === item.id ? { ...row, completed: !row.completed } : row,
      ),
    );
    try {
      const updated = await authorize((credentials) =>
        workspaceApi.updateTask(credentials, item.id, {
          version: item.version,
          completed: !item.completed,
        }),
      );
      setItems((previous) =>
        previous
          .map((row) => (row.id === updated.id ? updated : row))
          .filter(
            (row) =>
              !filters.completed || String(row.completed) === filters.completed,
          ),
      );
      await load();
    } catch (err) {
      setItems((previous) =>
        previous.map((row) => (row.id === item.id ? item : row)),
      );
      setError(errorMessage(err));
      if (err instanceof ApiError && err.status === 409) setConflict(true);
    } finally {
      locked.current = false;
      setPending("");
      setMutating(false);
    }
  }
  return (
    <section className="task-board" aria-labelledby="tasks-heading">
      <header className="board-heading">
        <div>
          <span className="eyebrow">MAKE ROOM FOR WHAT MATTERS</span>
          <h1 id="tasks-heading">{title}</h1>
          <p className="subtitle">
            One thing at a time. A little further each day.
          </p>
          {today && (
            <p className="plan-day" role="status">
              Amsterdam time / Europe/Amsterdam ·{" "}
              {day?.plan_date || "Checking the date…"}
            </p>
          )}
        </div>
        <button
          className="button primary"
          onClick={() => setEditor("new")}
          disabled={!!pending || (today && (loading || !!error || !day))}
        >
          ＋ New task
        </button>
      </header>
      {error && (
        <div className="notice error" role="alert">
          {error}
          {conflict && (
            <p>
              The completion change was undone. Refresh the list and review the
              latest version before trying again.
            </p>
          )}
          <button
            className="text-button"
            disabled={loading || !!pending}
            onClick={() => void load()}
          >
            Refresh list
          </button>
        </div>
      )}
      <div className="list-summary">
        <span>
          {items.length} {items.length === 1 ? "task" : "tasks"} loaded
          {cursor ? " · More to explore" : ""}
        </span>
        <button
          className="text-button"
          disabled={loading || !!pending}
          onClick={() => void load()}
        >
          Refresh
        </button>
      </div>
      {loading && (
        <p role="status" className="loading">
          Loading tasks…
        </p>
      )}
      {!loading && !error && items.length === 0 && (
        <div className="empty-state">
          <span aria-hidden="true">○</span>
          <h2>No tasks here yet</h2>
          <p>Start with one small task, or try a different filter.</p>
        </div>
      )}
      <ul className="task-list" aria-busy={loading}>
        {items.map((item) => (
          <li
            key={item.id}
            className={`task-row ${item.completed ? "is-complete" : ""}`}
          >
            <input
              className="task-check"
              type="checkbox"
              checked={item.completed}
              aria-label={`${item.completed ? "Mark incomplete" : "Complete task"}: ${item.title}`}
              disabled={loading || !!pending || conflict}
              onChange={() => void toggle(item)}
            />
            <div className="task-content">
              <h2>{item.title}</h2>
              {item.description && (
                <p className="task-description">{item.description}</p>
              )}
              <div className="task-meta">
                <span className={`priority priority-${item.priority}`}>
                  {
                    ["", "Low priority", "Medium priority", "High priority"][
                      item.priority
                    ]
                  }
                </span>
                <span>
                  {projects.find((p) => p.id === item.project_id)?.name ||
                    (item.plan_date
                      ? "Personal plan"
                      : item.project_id
                        ? "Project unavailable"
                        : "Unassigned")}
                </span>
                {item.plan_date && (
                  <time dateTime={item.plan_date}>
                    Planned for: {item.plan_date} (Amsterdam)
                  </time>
                )}
                {item.due_date && (
                  <time
                    className={
                      !item.completed && Date.parse(item.due_date) < now
                        ? "overdue"
                        : ""
                    }
                    dateTime={item.due_date}
                  >
                    {displayDate(item.due_date)}
                  </time>
                )}
                {pending === item.id && <span role="status">Syncing…</span>}
              </div>
            </div>
            <div className="row-actions">
              <button
                className="text-button"
                disabled={loading || !!pending}
                aria-label={`Edit task: ${item.title}`}
                onClick={() => setEditor(item)}
              >
                Edit
              </button>
              <button
                className="text-button danger-text"
                disabled={loading || !!pending}
                aria-label={`Delete task: ${item.title}`}
                onClick={() => setDeleting(item)}
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
            disabled={loading || !!pending}
            onClick={() => void load(cursor)}
          >
            Load more
          </button>
        </div>
      )}
      {editor && (
        <TaskEditor
          task={editor === "new" ? undefined : editor}
          projects={projects}
          defaultProject={filters.project}
          todayDate={today ? day?.plan_date : undefined}
          refreshToday={today ? () => load() : undefined}
          close={() => setEditor(null)}
          saved={() => {
            setEditor(null);
            void load();
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          title="Delete task"
          name={deleting.title}
          close={() => setDeleting(null)}
          remove={async () => {
            await authorize((credentials) =>
              workspaceApi.deleteTask(credentials, deleting.id),
            );
            await load();
          }}
        />
      )}
    </section>
  );
}
