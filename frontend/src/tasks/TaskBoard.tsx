import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useAuth } from "../auth/AuthProvider";
import { workspaceApi, type Filters } from "../api/workspace";
import { errorMessage, ApiError } from "../api/client";
import type { Task, Project, TodayPage } from "../api/types";
import { Icon } from "../components/Icon";
import { TaskDetails } from "./TaskDetails";
import { TaskEditor } from "./TaskEditor";
import { DeleteDialog } from "./DeleteDialog";

export function TaskBoard({
  today = false,
  filters,
  projects,
  title,
  toolbar,
  onAccessLost,
  selectedId,
  selectTask,
}: {
  toolbar?: ReactNode;
  onAccessLost?(): void;
  selectedId?: string;
  selectTask?(id: string): void;
  today?: boolean;
  filters: Filters;
  projects: Project[];
  title: string;
}) {
  const { authorize } = useAuth();
  const [localSelection, setLocalSelection] = useState("");
  const taskId = selectedId ?? localSelection;
  const choose = selectTask ?? setLocalSelection;
  const [revision, setRevision] = useState(0);
  const lostRef = useRef(onAccessLost);
  const chooseRef = useRef(choose);
  useEffect(() => {
    chooseRef.current = choose;
    lostRef.current = onAccessLost;
  }, [choose, onAccessLost]);
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
          if (dayRef.current && dayRef.current !== response.plan_date)
            chooseRef.current("");
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
        setRevision((value) => value + 1);
        setNow(Date.now());
        setCursor(result.next);
        setConflict(false);
        return "plan_date" in response ? response.plan_date : null;
      } catch (err) {
        if (!request.signal.aborted) {
          if (
            filters.project &&
            filters.project !== "none" &&
            err instanceof ApiError &&
            [403, 404].includes(err.status ?? 0)
          ) {
            setItems([]);
            chooseRef.current("");
            lostRef.current?.();
            return null;
          }
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
    }
  }
  return (
    <section className="task-board" aria-labelledby="tasks-heading">
      <header className="board-heading">
        <div>
          <h1 id="tasks-heading">{title}</h1>
          {today && (
            <p className="plan-day" role="status">
              Europe/Amsterdam · {day?.plan_date || "Checking the date…"}
            </p>
          )}
        </div>
        <button
          className="button primary"
          onClick={() => setEditor("new")}
          disabled={!!pending || (today && (loading || !!error || !day))}
        >
          <Icon name="plus" /> New task
        </button>
      </header>
      {toolbar}
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
      {loading && (
        <p role="status" className="loading">
          Loading tasks…
        </p>
      )}
      <div className={`task-grid ${taskId ? "has-detail" : ""}`}>
        <div className="task-table-wrap">
          <table className="task-table" aria-label="Tasks" aria-busy={loading}>
            <thead>
              <tr>
                <th className="check-col">
                  <span className="sr-only">Complete</span>
                </th>
                <th scope="col">Task / Project</th>
                <th scope="col" className="priority-col">
                  Priority
                </th>
                <th scope="col" className="due-col">
                  Due
                </th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr
                  key={item.id}
                  className={`${item.completed ? "is-complete" : ""} ${taskId === item.id ? "selected" : ""}`}
                >
                  <td>
                    <label className="completion">
                      <input
                        type="checkbox"
                        checked={item.completed}
                        aria-label={`${item.completed ? "Mark incomplete" : "Complete task"}: ${item.title}`}
                        disabled={loading || !!pending || conflict}
                        onChange={() => void toggle(item)}
                      />
                    </label>
                  </td>
                  <th scope="row">
                    <button
                      id={`task-${item.id}`}
                      className="task-title"
                      onClick={() => choose(item.id)}
                    >
                      {item.title}
                    </button>
                    <span className="task-project">
                      {projects.find((p) => p.id === item.project_id)?.name ||
                        (item.plan_date
                          ? "Personal plan"
                          : item.project_id
                            ? "Project unavailable"
                            : "Unassigned")}
                      {item.plan_date && (
                        <time dateTime={item.plan_date}>
                          {" "}
                          · Planned {item.plan_date}
                        </time>
                      )}
                    </span>
                    {pending === item.id && <span role="status">Syncing…</span>}
                  </th>
                  <td className="priority-col">
                    <span className={`priority priority-${item.priority}`}>
                      {["", "Low", "Medium", "High"][item.priority]}
                    </span>
                  </td>
                  <td className="due-col">
                    {item.due_date ? (
                      <time
                        dateTime={item.due_date}
                        className={
                          !item.completed && Date.parse(item.due_date) < now
                            ? "overdue"
                            : ""
                        }
                      >
                        {new Intl.DateTimeFormat("en-GB", {
                          day: "numeric",
                          month: "short",
                        }).format(new Date(item.due_date))}
                      </time>
                    ) : (
                      "—"
                    )}
                  </td>
                </tr>
              ))}
              {!loading && !error && !items.length && (
                <tr>
                  <td colSpan={4} className="empty-state">
                    No tasks
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {taskId && (
          <TaskDetails
            key={taskId}
            id={taskId}
            task={items.find((item) => item.id === taskId)}
            revision={revision}
            busy={!!pending}
            projects={projects}
            edit={setEditor}
            remove={setDeleting}
            close={() => {
              choose("");
              document.getElementById(`task-${taskId}`)?.focus();
            }}
          />
        )}
      </div>
      <div className="list-summary">
        <span>
          {items.length} {items.length === 1 ? "task" : "tasks"}
        </span>
        <button
          className="text-button"
          disabled={loading || !!pending}
          onClick={() => void load()}
        >
          Refresh
        </button>
      </div>
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
          saved={(saved) => {
            setEditor(null);
            setRevision((value) => value + 1);
            const matches =
              (!filters.completed ||
                String(saved.completed) === filters.completed) &&
              (!filters.priority ||
                String(saved.priority) === filters.priority) &&
              (!filters.project ||
                (filters.project === "none"
                  ? !saved.project_id
                  : saved.project_id === filters.project)) &&
              (!filters.planDate || saved.plan_date === filters.planDate);
            if (saved.id === taskId && !matches) choose("");
            setItems((previous) =>
              previous.map((item) => (item.id === saved.id ? saved : item)),
            );
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
            if (taskId === deleting.id) choose("");
            await load();
          }}
        />
      )}
    </section>
  );
}
