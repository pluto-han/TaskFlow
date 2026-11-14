import { useEffect, useRef, useState } from "react";
import type { Task, Project } from "../api/types";
import { workspaceApi } from "../api/workspace";
import { errorMessage } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { Icon } from "../components/Icon";
import { displayDate } from "./dates";

export function TaskDetails({
  id,
  task,
  revision,
  projects,
  busy = false,
  edit,
  remove,
  close,
}: {
  id: string;
  task?: Task;
  revision: number;
  projects: Project[];
  busy?: boolean;
  edit(task: Task): void;
  remove(task: Task): void;
  close(): void;
}) {
  const { authorize } = useAuth();
  const [remote, setRemote] = useState<Task | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, []);
  useEffect(() => {
    if (task) return;
    const controller = new AbortController();
    void authorize((credentials) =>
      workspaceApi.task(credentials, id, controller.signal),
    )
      .then((value) => {
        if (!controller.signal.aborted) {
          setRemote(value);
          setError("");
        }
      })
      .catch((err) => {
        if (!controller.signal.aborted) {
          setRemote(null);
          setError(errorMessage(err));
        }
      });
    return () => controller.abort();
  }, [id, task, revision, retry, authorize]);
  const item = task ?? remote;
  return (
    <aside className="task-detail" aria-label="Task details">
      <button
        className="icon-button detail-close"
        aria-label="Close task details"
        onClick={close}
      >
        <Icon name="x" />
      </button>
      {!item ? (
        <>
          <h2 ref={heading} tabIndex={-1}>
            Task
          </h2>
          <p role={error ? "alert" : "status"}>{error || "Loading task…"}</p>
          {error && (
            <button
              className="button secondary"
              onClick={() => setRetry(retry + 1)}
            >
              Retry
            </button>
          )}
        </>
      ) : (
        <>
          <h2 ref={heading} tabIndex={-1} className="detail-title">
            {item.title}
          </h2>
          {item.description && (
            <p className="task-description">{item.description}</p>
          )}
          <dl className="detail-fields">
            <div>
              <dt>Status</dt>
              <dd>{item.completed ? "Completed" : "Open"}</dd>
            </div>
            <div>
              <dt>Project</dt>
              <dd>
                {projects.find((p) => p.id === item.project_id)?.name ??
                  (item.plan_date
                    ? "Personal plan"
                    : item.project_id
                      ? "Project unavailable"
                      : "Unassigned")}
              </dd>
            </div>
            <div>
              <dt>Priority</dt>
              <dd>{["", "Low", "Medium", "High"][item.priority]}</dd>
            </div>
            {item.plan_date && (
              <div>
                <dt>Planned date</dt>
                <dd>{item.plan_date}</dd>
              </div>
            )}
            <div>
              <dt>Due</dt>
              <dd>{item.due_date ? displayDate(item.due_date) : "Not set"}</dd>
            </div>
          </dl>
          <div className="detail-actions">
            <button
              className="button secondary"
              disabled={busy}
              aria-label={`Edit task: ${item.title}`}
              onClick={() => edit(item)}
            >
              Edit task
            </button>
            <button
              className="button danger"
              disabled={busy}
              aria-label={`Delete task: ${item.title}`}
              onClick={() => remove(item)}
            >
              Delete
            </button>
          </div>
        </>
      )}
    </aside>
  );
}
