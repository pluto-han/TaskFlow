import { useCallback, useState } from "react";
import {
  Link,
  useLocation,
  useNavigate,
  useOutletContext,
  useParams,
} from "react-router";
import type { WorkspaceContext } from "../layouts/WorkspaceLayout";
import { returnTarget } from "../auth/navigation";
import { useAuth } from "../auth/AuthProvider";
import { workspaceApi } from "../api/workspace";
import { ProjectSettings } from "../projects/ProjectSettings";
import { ProjectEditor } from "../projects/ProjectEditor";
import { DeleteDialog } from "../tasks/DeleteDialog";

export function ProjectSettingsPage() {
  const { projectId } = useParams();
  const { projects, loading, projectError, refreshProjects } =
    useOutletContext<WorkspaceContext>();
  const { authorize } = useAuth();
  const navigate = useNavigate();
  const { state } = useLocation();
  const leaveSettings = useCallback(() => navigate("/tasks"), [navigate]);
  const taskPath = `/projects/${encodeURIComponent(projectId ?? "")}/tasks`;
  const remembered = returnTarget(state?.workspaceReturnTo ?? null);
  const back =
    remembered === taskPath || remembered.startsWith(`${taskPath}?`)
      ? remembered
      : taskPath;
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const project = projects.find((p) => p.id === projectId);
  if (!project)
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
  return (
    <section className="settings-content project-settings">
      <header className="board-heading">
        <h1>{project.name}</h1>
        <Link className="text-button" to={back}>
          Back to tasks
        </Link>
      </header>
      {project.description && (
        <p className="project-description">{project.description}</p>
      )}
      {project.role === "owner" && (
        <div className="settings-row">
          <h2>Project</h2>
          <div className="actions-buttons">
            <button
              className="button secondary"
              onClick={() => setEditing(true)}
            >
              Edit project
            </button>
            <button className="button danger" onClick={() => setDeleting(true)}>
              Delete project
            </button>
          </div>
        </div>
      )}
      <ProjectSettings
        project={project}
        changed={refreshProjects}
        close={leaveSettings}
      />
      {editing && (
        <ProjectEditor
          project={project}
          close={() => setEditing(false)}
          saved={() => {
            setEditing(false);
            void refreshProjects();
          }}
        />
      )}
      {deleting && (
        <DeleteDialog
          title="Delete project"
          name={project.name}
          project
          close={() => setDeleting(false)}
          remove={async () => {
            await authorize((credentials) =>
              workspaceApi.deleteProject(credentials, project.id),
            );
            await refreshProjects();
            navigate("/tasks");
          }}
        />
      )}
    </section>
  );
}
