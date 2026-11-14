import { useCallback, useEffect, useRef, useState } from "react";
import { Link, Navigate, NavLink, Outlet, useLocation } from "react-router";
import { useAuth } from "../auth/AuthProvider";
import { loginTarget, returnTarget } from "../auth/navigation";
import { workspaceApi } from "../api/workspace";
import { errorMessage } from "../api/client";
import type { Project } from "../api/types";
import { Icon, type IconName } from "../components/Icon";
import { Modal } from "../components/Modal";
import { ProjectEditor } from "../projects/ProjectEditor";
import { PublicLayout } from "./PublicLayout";

export type WorkspaceContext = {
  projects: Project[];
  loading: boolean;
  projectError: string;
  refreshProjects(): Promise<void>;
};
const taskLinks: [string, string, IconName][] = [
  ["/tasks", "All tasks", "list"],
  ["/tasks/today", "Today", "calendar-days"],
  ["/tasks/unassigned", "Unassigned", "inbox"],
  ["/tasks/recurring", "Recurring tasks", "repeat-2"],
];
const settingLinks: [string, string, IconName][] = [
  ["/settings/account", "Account", "user-round"],
  ["/settings/security", "Security", "shield-check"],
  ["/settings/integrations", "Integrations", "plug"],
];
export function WorkspaceLayout() {
  const { user, authorize } = useAuth();
  const { pathname, search, state } = useLocation();
  const userId = user?.id;
  const settings = pathname.startsWith("/settings") || pathname === "/profile";
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [projectError, setProjectError] = useState("");
  const [creating, setCreating] = useState(false);
  const [menu, setMenu] = useState(false);
  const request = useRef<AbortController | null>(null);
  const refreshProjects = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    try {
      const rows = await authorize((credentials) =>
        workspaceApi.projects(credentials, controller.signal),
      );
      if (!controller.signal.aborted) {
        setProjects(rows);
        setProjectError("");
      }
    } catch (err) {
      if (!controller.signal.aborted) setProjectError(errorMessage(err));
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [authorize]);
  useEffect(() => {
    // Load the project directory only in task views; account pages have independent data.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (userId && !settings) void refreshProjects();
    return () => request.current?.abort();
  }, [userId, settings, refreshProjects]);
  if (!user) return <Navigate to={loginTarget(pathname + search)} replace />;
  const remembered = returnTarget(state?.workspaceReturnTo ?? null);
  const taskView = (path: string) =>
    path.startsWith("/tasks") || /^\/projects\/[^/]+\/tasks(?:\?|$)/.test(path);
  const tasks = taskView(pathname)
    ? returnTarget(pathname + search)
    : taskView(remembered)
      ? remembered
      : "/tasks";
  const navState = { workspaceReturnTo: tasks };
  const filters = new URLSearchParams(search);
  const viewQuery = new URLSearchParams();
  for (const key of ["completed", "priority"])
    if (filters.has(key)) viewQuery.set(key, filters.get(key)!);
  function sidebar() {
    return (
      <nav
        className="sidebar-navigation"
        aria-label={settings ? "Settings navigation" : "Project navigation"}
      >
        {(settings ? settingLinks : taskLinks).map(([to, label, icon]) => (
          <NavLink
            key={to}
            end
            to={
              to +
              (!settings && to !== "/tasks/recurring" && viewQuery.size
                ? `?${viewQuery}`
                : "")
            }
            state={navState}
            onClick={() => setMenu(false)}
          >
            <Icon name={icon} />
            {label}
          </NavLink>
        ))}
        {!settings && (
          <>
            <div className="sidebar-heading">
              <h2>Projects</h2>
              <div>
                <button
                  className="icon-button"
                  aria-label="Refresh projects"
                  disabled={loading}
                  onClick={() => void refreshProjects()}
                >
                  <Icon name="refresh-cw" />
                </button>
                <button
                  className="icon-button"
                  aria-label="New project"
                  onClick={() => {
                    setMenu(false);
                    setCreating(true);
                  }}
                >
                  <Icon name="plus" />
                </button>
              </div>
            </div>
            {loading && <p role="status">Loading projects…</p>}
            {projectError && (
              <p className="notice error" role="alert">
                {projectError}
                <button onClick={() => void refreshProjects()}>Retry</button>
              </p>
            )}
            {!loading && !projectError && !projects.length && (
              <p className="hint">No projects</p>
            )}
            {projects.map((project) => (
              <Link
                key={project.id}
                aria-current={
                  pathname.startsWith(
                    `/projects/${encodeURIComponent(project.id)}/`,
                  )
                    ? "page"
                    : undefined
                }
                to={`/projects/${encodeURIComponent(project.id)}/tasks${viewQuery.size ? `?${viewQuery}` : ""}`}
                onClick={() => setMenu(false)}
              >
                <Icon name="folder" />
                {project.name}
              </Link>
            ))}
          </>
        )}
        {settings && (
          <Link
            className="sidebar-back"
            to={tasks}
            onClick={() => setMenu(false)}
          >
            <Icon name="arrow-left" />
            Back to tasks
          </Link>
        )}
      </nav>
    );
  }
  return (
    <PublicLayout
      workspace
      navigation={
        <>
          <nav className="app-nav" aria-label="Main navigation">
            <Link to={tasks} aria-current={!settings ? "page" : undefined}>
              Tasks
            </Link>
            <Link
              to="/settings/account"
              state={navState}
              aria-current={settings ? "page" : undefined}
            >
              Settings
            </Link>
          </nav>
          <div className="workspace-identity">
            <span title={user.email}>{user.email}</span>
            <button
              className="button secondary menu-trigger"
              aria-label="Open navigation"
              aria-expanded={menu}
              onClick={() => setMenu(true)}
            >
              Menu
            </button>
          </div>
        </>
      }
    >
      <div className="workspace">
        <aside className="project-sidebar">{sidebar()}</aside>
        <div className="workspace-main">
          <Outlet
            context={
              {
                projects,
                loading,
                projectError,
                refreshProjects,
              } satisfies WorkspaceContext
            }
          />
        </div>
      </div>
      {menu && (
        <Modal title="Navigation" close={() => setMenu(false)}>
          {sidebar()}
        </Modal>
      )}
      {creating && (
        <ProjectEditor
          close={() => setCreating(false)}
          saved={() => {
            setCreating(false);
            void refreshProjects();
          }}
        />
      )}
    </PublicLayout>
  );
}
