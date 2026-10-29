import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "../auth/AuthProvider";
import { workspaceApi, type Filters } from "../api/workspace";
import { errorMessage, ApiError } from "../api/client";
import type { Task, Project } from "../api/types";
import { TaskEditor } from "./TaskEditor";
import { DeleteDialog } from "./DeleteDialog";
import { displayDate } from "./dates";

export function TaskBoard({
  filters,
  projects,
  title,
  setMutating,
}: {
  filters: Filters;
  projects: Project[];
  title: string;
  setMutating(busy: boolean): void;
}) {
  const { authorize } = useAuth();
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
  const load = useCallback(
    async (after = "") => {
      controller.current?.abort();
      const request = new AbortController();
      controller.current = request;
      setLoading(true);
      setError("");
      try {
        const result = await authorize((token) =>
          workspaceApi.tasks(token, filters, after, request.signal),
        );
        if (request.signal.aborted) return;
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
      } catch (err) {
        if (!request.signal.aborted) setError(errorMessage(err));
      } finally {
        if (!request.signal.aborted) setLoading(false);
      }
    },
    [authorize, filters],
  );
  useEffect(() => {
    // Fetch state belongs to this keyed filter view; cleanup rejects stale pages.
    // Explicit fetch boundary; load also supports user-triggered refresh.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
    return () => controller.current?.abort();
  }, [load]);

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
      const updated = await authorize((token) =>
        workspaceApi.updateTask(token, item.id, {
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
          <p className="subtitle">一次一件事，向前一点点。</p>
        </div>
        <button
          className="button primary"
          onClick={() => setEditor("new")}
          disabled={!!pending}
        >
          ＋ 新建任务
        </button>
      </header>
      {error && (
        <div className="notice error" role="alert">
          {error}
          {conflict && (
            <p>完成状态已回滚。请刷新列表，查看最新内容后再操作。</p>
          )}
          <button
            className="text-button"
            disabled={loading || !!pending}
            onClick={() => void load()}
          >
            刷新列表
          </button>
        </div>
      )}
      <div className="list-summary">
        <span>
          已加载 {items.length} 项{cursor ? " · 还有更多" : ""}
        </span>
        <button
          className="text-button"
          disabled={loading || !!pending}
          onClick={() => void load()}
        >
          刷新
        </button>
      </div>
      {loading && (
        <p role="status" className="loading">
          正在加载任务…
        </p>
      )}
      {!loading && !error && items.length === 0 && (
        <div className="empty-state">
          <span aria-hidden="true">○</span>
          <h2>这里还没有任务</h2>
          <p>新建一个任务，或调整筛选条件。</p>
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
              aria-label={`${item.completed ? "标记未完成" : "完成任务"}：${item.title}`}
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
                  {["", "低优先级", "中优先级", "高优先级"][item.priority]}
                </span>
                <span>
                  {projects.find((p) => p.id === item.project_id)?.name ||
                    (item.project_id ? "项目不可用" : "未归类")}
                </span>
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
                {pending === item.id && <span role="status">正在同步…</span>}
              </div>
            </div>
            <div className="row-actions">
              <button
                className="text-button"
                disabled={loading || !!pending}
                aria-label={`编辑任务：${item.title}`}
                onClick={() => setEditor(item)}
              >
                编辑
              </button>
              <button
                className="text-button danger-text"
                disabled={loading || !!pending}
                aria-label={`删除任务：${item.title}`}
                onClick={() => setDeleting(item)}
              >
                删除
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
            加载更多
          </button>
        </div>
      )}
      {editor && (
        <TaskEditor
          task={editor === "new" ? undefined : editor}
          projects={projects}
          defaultProject={filters.project}
          close={() => setEditor(null)}
          saved={() => {
            setEditor(null);
            void load();
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          title="删除任务"
          name={deleting.title}
          close={() => setDeleting(null)}
          remove={async () => {
            await authorize((token) =>
              workspaceApi.deleteTask(token, deleting.id),
            );
            await load();
          }}
        />
      )}
    </section>
  );
}
