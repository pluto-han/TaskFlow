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
      const result = await authorize((token) =>
        workspaceApi.projects(token, signal),
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
      <aside className="project-sidebar" aria-label="项目导航">
        <span className="eyebrow">你的工作空间</span>
        <button
          className={`project-link ${!recurrences && !today && !project ? "selected" : ""}`}
          disabled={mutating}
          onClick={() => {
            setRecurrences(false);
            setToday(false);
            filter({ project: "", completed, priority });
          }}
        >
          全部任务 <span>↗</span>
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
          今日任务
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
          未归类
        </button>
        <button
          className={`project-link ${recurrences ? "selected" : ""}`}
          onClick={() => {
            setRecurrences(true);
            setToday(false);
          }}
          disabled={mutating}
        >
          循环任务
        </button>
        <div className="sidebar-heading">
          <h2>项目</h2>
          <button
            className="icon-button"
            aria-label="刷新项目列表"
            disabled={mutating}
            onClick={() => void loadProjects()}
          >
            ↻
          </button>
          {!recurrences && !today && (
            <button
              className="icon-button"
              aria-label="新建项目"
              onClick={() => setEditor("new")}
              disabled={mutating}
            >
              ＋
            </button>
          )}
        </div>
        {loading && <p role="status">正在加载项目…</p>}
        {projectError && (
          <div role="alert" className="notice error">
            {projectError}
            <button className="text-button" onClick={() => void loadProjects()}>
              重试项目列表
            </button>
          </div>
        )}
        {!loading && !projectError && projects.length === 0 && (
          <p className="hint">
            {today || recurrences
              ? "暂无项目。可在全部任务中创建项目。"
              : "还没有项目，试着创建一个。"}
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
                  状态
                  <select
                    aria-label="筛选完成状态"
                    value={completed}
                    disabled={mutating}
                    onChange={(e) =>
                      filter({ project, completed: e.target.value, priority })
                    }
                  >
                    <option value="">全部状态</option>
                    <option value="false">未完成</option>
                    <option value="true">已完成</option>
                  </select>
                </label>
                <label>
                  优先级
                  <select
                    aria-label="筛选优先级"
                    value={priority}
                    disabled={mutating}
                    onChange={(e) =>
                      filter({ project, completed, priority: e.target.value })
                    }
                  >
                    <option value="">全部优先级</option>
                    <option value="3">高</option>
                    <option value="2">中</option>
                    <option value="1">低</option>
                  </select>
                </label>
                {!recurrences && !today && !project && (
                  <div className="plan-date-filter">
                    <label htmlFor="plan-date-filter">计划日期（历史）</label>
                    <div className="plan-date-control">
                      <input
                        id="plan-date-filter"
                        aria-label="筛选计划日期"
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
                          清除日期筛选
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
                    成员与邀请
                  </button>
                  {selected.role === "owner" && (
                    <>
                      <button
                        className="text-button"
                        disabled={mutating}
                        onClick={() => setEditor(selected)}
                      >
                        编辑项目
                      </button>
                      <button
                        className="text-button danger-text"
                        disabled={mutating}
                        onClick={() => setDeleting(selected)}
                      >
                        删除项目
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
                  ? "今日任务"
                  : selected?.name ||
                    (project === "none" ? "未归类" : "全部任务")
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
          title="删除项目"
          name={deleting.name}
          project
          close={() => setDeleting(null)}
          remove={async () => {
            await authorize((token) =>
              workspaceApi.deleteProject(token, deleting.id),
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
