import { useMemo } from "react";
import {
  Link,
  Navigate,
  useLocation,
  useNavigate,
  useOutletContext,
  useParams,
  useSearchParams,
} from "react-router";
import type { WorkspaceContext } from "../layouts/WorkspaceLayout";
import { TaskBoard } from "../tasks/TaskBoard";
import { RecurrenceBoard } from "../recurrences/RecurrenceBoard";

export function WorkspacePage() {
  const { pathname, search } = useLocation();
  const { projectId } = useParams();
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const { projects, loading, projectError, refreshProjects } =
    useOutletContext<WorkspaceContext>();
  const today = pathname === "/tasks/today";
  const recurring = pathname === "/tasks/recurring";
  const project = projectId ?? (pathname === "/tasks/unassigned" ? "none" : "");
  const { canonical, filters } = useMemo(() => {
    const canonical = new URLSearchParams(params);
    for (const key of [...canonical.keys()])
      if (!["completed", "priority", "plan_date", "task"].includes(key))
        canonical.delete(key);
    for (const key of ["completed", "priority", "plan_date"]) {
      const values = canonical.getAll(key),
        value = values[0] ?? "";
      const valid =
        key === "completed"
          ? ["true", "false"].includes(value)
          : key === "priority"
            ? ["1", "2", "3"].includes(value)
            : !recurring && !today && !project && validPlanDate(value);
      if (values.length !== 1 || !valid || recurring) canonical.delete(key);
    }
    if (
      recurring ||
      canonical.getAll("task").length !== 1 ||
      !canonical.get("task")
    )
      canonical.delete("task");
    return {
      canonical,
      filters: {
        project,
        completed: canonical.get("completed") ?? "",
        priority: canonical.get("priority") ?? "",
        planDate: canonical.get("plan_date") ?? "",
      },
    };
  }, [params, project, recurring, today]);
  // Keep list data stable when only its selected detail changes.
  const taskFilters = useMemo(
    () => ({
      project: filters.project,
      completed: filters.completed,
      priority: filters.priority,
      planDate: filters.planDate,
    }),
    [filters.project, filters.completed, filters.priority, filters.planDate],
  );
  function changeFilter(key: string, value: string) {
    const next = new URLSearchParams(canonical);
    next.delete("task");
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next);
  }
  function selectTask(id: string) {
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (id) next.set("task", id);
      else next.delete("task");
      return next;
    });
  }
  if (canonical.toString() !== params.toString())
    return <Navigate to={{ pathname, search: canonical.toString() }} replace />;
  const selected = projects.find((p) => p.id === project);
  if (projectId && !selected)
    return (
      <section>
        <h1>Project</h1>
        <p role={loading ? "status" : "alert"}>
          {loading
            ? "Loading project…"
            : projectError || "Project is unavailable."}
        </p>
        <Link to="/tasks">All tasks</Link>
      </section>
    );
  if (recurring) return <RecurrenceBoard />;
  const title = today
    ? "Today"
    : (selected?.name ?? (project === "none" ? "Unassigned" : "All tasks"));
  return (
    <TaskBoard
      key={JSON.stringify([
        project,
        filters.completed,
        filters.priority,
        filters.planDate,
        today,
      ])}
      today={today}
      filters={taskFilters}
      projects={projects}
      title={title}
      onAccessLost={() => {
        void refreshProjects();
        navigate("/tasks", { replace: true });
      }}
      selectedId={canonical.get("task") ?? ""}
      selectTask={selectTask}
      toolbar={
        <>
          <div className="view-tabs" aria-label="Task status">
            {[
              ["", "All tasks"],
              ["false", "Open"],
              ["true", "Completed"],
            ].map(([value, label]) => (
              <button
                key={label}
                aria-pressed={filters.completed === value}
                onClick={() => changeFilter("completed", value)}
              >
                {label}
              </button>
            ))}
          </div>
          <div className="workspace-toolbar">
            <div className="filters">
              <label>
                Priority
                <select
                  aria-label="Filter by priority"
                  value={filters.priority}
                  onChange={(e) => changeFilter("priority", e.target.value)}
                >
                  <option value="">All priorities</option>
                  <option value="3">High</option>
                  <option value="2">Medium</option>
                  <option value="1">Low</option>
                </select>
              </label>
              {!today && !project && (
                <label className="plan-date-filter">
                  Planned date
                  <input
                    aria-label="Filter by planned date"
                    type="date"
                    min="0001-01-01"
                    max="9999-12-31"
                    value={filters.planDate}
                    onChange={(e) => changeFilter("plan_date", e.target.value)}
                  />
                  {filters.planDate && (
                    <button
                      className="text-button"
                      onClick={() => changeFilter("plan_date", "")}
                    >
                      Clear date filter
                    </button>
                  )}
                </label>
              )}
            </div>
            {selected && (
              <Link
                className="text-button"
                to={`/projects/${encodeURIComponent(selected.id)}/settings`}
                state={{ workspaceReturnTo: pathname + search }}
              >
                Project settings
              </Link>
            )}
          </div>
        </>
      }
    />
  );
}
function validPlanDate(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || value < "0001-01-01") return false;
  const date = new Date(`${value}T00:00:00Z`);
  return (
    Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value
  );
}
