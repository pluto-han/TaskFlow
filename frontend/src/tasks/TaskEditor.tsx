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
      setError("请填写任务标题。");
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
        await authorize((token) =>
          workspaceApi.createToday(token, {
            plan_date: draftDay,
            title: title.trim(),
            description,
            priority,
            due_date: dueDate,
          }),
        );
      } else if (!base)
        await authorize((token) =>
          workspaceApi.createTask(token, {
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
        await authorize((token) =>
          workspaceApi.updateTask(token, base.id, patch),
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
      setLatest(await authorize((token) => workspaceApi.task(token, base.id)));
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
    <Modal title={base ? "编辑任务" : "新建任务"} busy={busy} close={close}>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      {!base && draftDay && (dayConflict || draftDay !== todayDate) && (
        <section className="notice" aria-label="计划日期变化">
          <p>荷兰日期已变化，草稿已保留。确认新日期后再保存，不会自动提交。</p>
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
                } else setError("暂时无法确认今天的日期，请重试。草稿已保留。");
              } catch (err) {
                setError(errorMessage(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            刷新并确认计划日期
          </button>
        </section>
      )}
      {(base?.plan_date || draftDay) && (
        <p className="hint">
          计划日期：{base?.plan_date || draftDay} · 荷兰时间 /
          Europe/Amsterdam。个人计划不能加入项目。
        </p>
      )}
      {conflict && (
        <section className="notice" aria-label="版本冲突">
          <p>
            你的输入已保留。请先读取最新内容，确认后重新编辑；不会自动覆盖其他修改。
          </p>
          <button
            className="button secondary"
            disabled={busy}
            type="button"
            onClick={() => void fetchLatest()}
          >
            读取最新版本
          </button>
          {latest && (
            <div className="comparison">
              <strong>服务器最新内容</strong>
              <p>{latest.title}</p>
              <p className="preserve-lines">{latest.description || "无描述"}</p>
              <p>
                优先级 {latest.priority} ·{" "}
                {latest.completed ? "已完成" : "未完成"} ·{" "}
                {latest.due_date ? displayDate(latest.due_date) : "无截止日期"}
              </p>
              <p>
                项目：
                {projects.find((p) => p.id === latest.project_id)?.name ||
                  "未归类"}
              </p>
              <button
                type="button"
                className="button secondary"
                disabled={busy}
                onClick={useLatest}
              >
                放弃当前输入，使用最新内容
              </button>
            </div>
          )}
        </section>
      )}
      {project && (
        <p className="notice">
          项目内的任务对所有当前成员可见，成员可以编辑和删除。只有创建者可以移动或解除归类。
        </p>
      )}
      {base && !base.can_move && !base.plan_date && (
        <p className="hint">你不是这项任务的创建者，不能改变所属项目。</p>
      )}
      <form onSubmit={(event) => void submit(event)} aria-busy={busy}>
        <div className="field">
          <label htmlFor="task-title">任务标题</label>
          <input
            id="task-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            disabled={busy}
          />
        </div>
        <div className="field">
          <label htmlFor="task-description">描述</label>
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
            <label htmlFor="task-priority">优先级</label>
            <select
              id="task-priority"
              value={priority}
              onChange={(e) => setPriority(Number(e.target.value))}
              disabled={busy}
            >
              <option value={1}>低</option>
              <option value={2}>中</option>
              <option value={3}>高</option>
            </select>
          </div>
          {!draftDay && !base?.plan_date && (
            <div className="field">
              <label htmlFor="task-project">所属项目</label>
              <select
                id="task-project"
                value={project}
                onChange={(e) => setProject(e.target.value)}
                disabled={busy || (!!base && !base.can_move)}
              >
                <option value="">未归类</option>
                {project && !projects.some((p) => p.id === project) && (
                  <option value={project}>当前项目（请刷新项目列表）</option>
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
          <label htmlFor="task-due">截止日期（本地时间）</label>
          <input
            id="task-due"
            type="datetime-local"
            step="1"
            value={due}
            onChange={(e) => setDue(e.target.value)}
            disabled={busy}
          />
          <p className="hint">
            当前时区：{Intl.DateTimeFormat().resolvedOptions().timeZone}
            。留空表示无截止日期；重复的夏令时时刻按浏览器选择较早时刻。
          </p>
          {due && (
            <button
              type="button"
              className="text-button"
              disabled={busy}
              onClick={() => setDue("")}
            >
              清除截止日期
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
            取消
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
            {busy ? "正在保存…" : "保存任务"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
