import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Navigate } from "react-router";
import { useAuth } from "../auth/AuthProvider";
import { workspaceApi } from "../api/workspace";
import { errorMessage } from "../api/client";
import type { Project } from "../api/types";
import { SharingPanel } from "../tasks/SharingPanel";
import { TaskBoard } from "../tasks/TaskBoard";
import { ProjectEditor } from "../tasks/ProjectEditor";
import { DeleteDialog } from "../tasks/DeleteDialog";

import { RecurrenceBoard } from "../tasks/RecurrenceBoard";

export function WorkspacePage() {
  const { user } = useAuth();
  return user ? <Workspace key={user.id} /> : <Navigate to="/login" replace />;
}
function Workspace() {
  const { authorize } = useAuth();
  const [recurrences, setRecurrences] = useState(false);
  const [today, setToday] = useState(false);
  const [planDate, setPlanDate] = useState("");
  const [projects, setProjects] = useState<Project[]>([]);
  const [boardFilters, setBoardFilters] = useState({
    project: "",
    completed: "",
    priority: "",
  });
  const { project, completed, priority } = boardFilters;
  const taskFilters = useMemo(
    () => ({
      ...boardFilters,
      planDate: !recurrences && !today && !project ? planDate : "",
    }),
    [boardFilters, recurrences, today, project, planDate],
  );
  const [projectError, setProjectError] = useState("");
  const [loading, setLoading] = useState(true);
  const [mutating, setMutating] = useState(false);
  const [editor, setEditor] = useState<Project | "new" | null>(null);
  const [sharing, setSharing] = useState<Project | null>(null);
  const [deleting, setDeleting] = useState<Project | null>(null);
  const projectRequest = useRef<AbortController | null>(null);
  const loadProjects = useCallback(async () => {
    projectRequest.current?.abort();
    const controller = new AbortController();
    projectRequest.current = controller;
    const signal = controller.signal;
    try {
      const result = await authorize((credentials) =>
        workspaceApi.projects(credentials, signal),
      );
      if (!signal?.aborted) {
        setProjects(result);
        setProjectError("");
        setBoardFilters((current) =>
          current.project &&
          current.project !== "none" &&
          !result.some((p) => p.id === current.project)
            ? { ...current, project: "" }
            : current,
        );
      }
    } catch (err) {
      if (!signal?.aborted) setProjectError(errorMessage(err));
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, [authorize]);
  useEffect(() => {
    // This effect synchronizes the project collection with the server.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadProjects();
    return () => projectRequest.current?.abort();
  }, [loadProjects]);
  const selected = today ? undefined : projects.find((p) => p.id === project);
  function filter(next: {
    project: string;
    completed: string;
    priority: string;
  }) {
    setBoardFilters(next);
  }
  // A deleted selected project returns to all tasks via the explicit delete action.
  return (
    <div className="workspace">
      <aside className="project-sidebar" aria-label="Project navigation">
        <span className="eyebrow">Your workspace</span>
        <button
          className={`project-link ${!recurrences && !today && !project ? "selected" : ""}`}
          disabled={mutating}
          onClick={() => {
            setRecurrences(false);
            setToday(false);
            filter({ project: "", completed, priority });
          }}
        >
          All tasks <span>↗</span>
        </button>
        <button
          className={`project-link ${today ? "selected" : ""}`}
          aria-pressed={today}
          disabled={mutating}
          onClick={() => {
            setRecurrences(false);
            setToday(true);
            setPlanDate("");
            filter({ project: "", completed, priority });
          }}
        >
          Today
        </button>
        <button
          className={`project-link ${!recurrences && !today && project === "none" ? "selected" : ""}`}
          disabled={mutating}
          onClick={() => {
            setRecurrences(false);
            setToday(false);
            filter({ project: "none", completed, priority });
          }}
        >
          Unassigned
        </button>
        <button
          className={`project-link ${recurrences ? "selected" : ""}`}
          onClick={() => {
            setRecurrences(true);
            setToday(false);
          }}
          disabled={mutating}
        >
          Recurring tasks
        </button>
        <div className="sidebar-heading">
          <h2>Projects</h2>
          <button
            className="icon-button"
            aria-label="Refresh projects"
            disabled={mutating}
            onClick={() => void loadProjects()}
          >
            ↻
          </button>
          {!recurrences && !today && (
            <button
              className="icon-button"
              aria-label="New project"
              onClick={() => setEditor("new")}
              disabled={mutating}
            >
              ＋
            </button>
          )}
        </div>
        {loading && <p role="status">Loading projects…</p>}
        {projectError && (
          <div role="alert" className="notice error">
            {projectError}
            <button className="text-button" onClick={() => void loadProjects()}>
              Try loading projects again
            </button>
          </div>
        )}
        {!loading && !projectError && projects.length === 0 && (
          <p className="hint">
            {today || recurrences
              ? "No projects yet. You can create one from All tasks."
              : "Give your next idea a place to grow. Start a project."}
          </p>
        )}
        {projects.map((p) => (
          <button
            key={p.id}
            className={`project-link ${!recurrences && !today && project === p.id ? "selected" : ""}`}
            disabled={mutating}
            onClick={() => {
              setRecurrences(false);
              setToday(false);
              setPlanDate("");
              filter({ project: p.id, completed, priority });
            }}
          >
            <span className="project-dot" aria-hidden="true" />
            {p.name}
          </button>
        ))}
      </aside>
      <div className="workspace-main">
        {recurrences ? (
          <RecurrenceBoard />
        ) : (
          <>
            <div className="workspace-toolbar">
              <div className="filters">
                <label>
                  Status
                  <select
                    aria-label="Filter by status"
                    value={completed}
                    disabled={mutating}
                    onChange={(e) =>
                      filter({ project, completed: e.target.value, priority })
                    }
                  >
                    <option value="">All statuses</option>
                    <option value="false">Incomplete</option>
                    <option value="true">Completed</option>
                  </select>
                </label>
                <label>
                  Priority
                  <select
                    aria-label="Filter by priority"
                    value={priority}
                    disabled={mutating}
                    onChange={(e) =>
                      filter({ project, completed, priority: e.target.value })
                    }
                  >
                    <option value="">All priorities</option>
                    <option value="3">High</option>
                    <option value="2">Medium</option>
                    <option value="1">Low</option>
                  </select>
                </label>
                {!recurrences && !today && !project && (
                  <div className="plan-date-filter">
                    <label htmlFor="plan-date-filter">
                      Planned date (history)
                    </label>
                    <div className="plan-date-control">
                      <input
                        id="plan-date-filter"
                        aria-label="Filter by planned date"
                        type="date"
                        min="0001-01-01"
                        max="9999-12-31"
                        value={planDate}
                        disabled={mutating}
                        onChange={(e) => setPlanDate(e.target.value)}
                      />
                      {planDate && (
                        <button
                          className="text-button"
                          disabled={mutating}
                          onClick={() => setPlanDate("")}
                        >
                          Clear date filter
                        </button>
                      )}
                    </div>
                  </div>
                )}
              </div>
              {selected && (
                <div className="project-actions">
                  <button
                    className="text-button"
                    disabled={mutating}
                    onClick={() => setSharing(selected)}
                  >
                    Members & invitations
                  </button>
                  {selected.role === "owner" && (
                    <>
                      <button
                        className="text-button"
                        disabled={mutating}
                        onClick={() => setEditor(selected)}
                      >
                        Edit project
                      </button>
                      <button
                        className="text-button danger-text"
                        disabled={mutating}
                        onClick={() => setDeleting(selected)}
                      >
                        Delete project
                      </button>
                    </>
                  )}
                </div>
              )}
            </div>
            {selected?.description && (
              <p className="project-description">{selected.description}</p>
            )}
            <TaskBoard
              key={JSON.stringify([
                boardFilters,
                today,
                !project ? planDate : "",
              ])}
              today={today}
              filters={taskFilters}
              projects={projects}
              title={
                today
                  ? "Today"
                  : selected?.name ||
                    (project === "none" ? "Unassigned" : "All tasks")
              }
              setMutating={setMutating}
            />
          </>
        )}
      </div>
      {sharing && (
        <SharingPanel
          project={sharing}
          close={() => setSharing(null)}
          changed={() => void loadProjects()}
        />
      )}
      {editor && (
        <ProjectEditor
          project={editor === "new" ? undefined : editor}
          close={() => setEditor(null)}
          saved={() => {
            setEditor(null);
            void loadProjects();
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          title="Delete project"
          name={deleting.name}
          project
          close={() => setDeleting(null)}
          remove={async () => {
            await authorize((credentials) =>
              workspaceApi.deleteProject(credentials, deleting.id),
            );
            if (project === deleting.id)
              filter({ project: "", completed, priority });
            await loadProjects();
          }}
        />
      )}
    </div>
  );
}
