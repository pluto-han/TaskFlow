import { useState, type FormEvent } from "react";
import { Modal } from "../components/Modal";
import { useAuth } from "../auth/AuthProvider";
import { workspaceApi } from "../api/workspace";
import { errorMessage } from "../api/client";
import type { Project, UpdateProject } from "../api/types";

export function ProjectEditor({
  project,
  close,
  saved,
}: {
  project?: Project;
  close(): void;
  saved(): void;
}) {
  const { authorize } = useAuth();
  const [name, setName] = useState(project?.name || "");
  const [description, setDescription] = useState(project?.description || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    if (
      !name.trim() ||
      Array.from(name.trim()).length > 100 ||
      Array.from(description).length > 2000
    ) {
      setError("名称需为 1–100 个字符，描述最多 2000 个字符。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      if (project) {
        const patch: UpdateProject = {};
        if (name.trim() !== project.name) patch.name = name.trim();
        if (description !== project.description)
          patch.description = description;
        if (Object.keys(patch).length === 0) {
          close();
          return;
        }
        await authorize((token) =>
          workspaceApi.updateProject(token, project.id, patch),
        );
      } else
        await authorize((token) =>
          workspaceApi.createProject(token, { name: name.trim(), description }),
        );
      saved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title={project ? "编辑项目" : "新建项目"} busy={busy} close={close}>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      <form onSubmit={(event) => void submit(event)} aria-busy={busy}>
        <div className="field">
          <label htmlFor="project-name">项目名称</label>
          <input
            id="project-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            disabled={busy}
          />
        </div>
        <div className="field">
          <label htmlFor="project-description">项目描述</label>
          <textarea
            id="project-description"
            rows={3}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            disabled={busy}
          />
        </div>
        <div className="modal-actions">
          <button
            type="button"
            className="button secondary"
            disabled={busy}
            onClick={close}
          >
            取消
          </button>
          <button type="submit" className="button primary" disabled={busy}>
            {busy ? "正在保存…" : "保存项目"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
