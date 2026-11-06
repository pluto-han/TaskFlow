import { request } from "./client";
import type {
  Task,
  TodayPage,
  CreateTodayTask,
  CreateTask,
  UpdateTask,
  Project,
  CreateProject,
  UpdateProject,
} from "./types";

export type Filters = {
  project: string;
  completed: string;
  priority: string;
  planDate?: string;
};
export const workspaceApi = {
  today(token: string, filters: Filters, cursor = "", signal?: AbortSignal) {
    const params = new URLSearchParams({ limit: "20" });
    if (filters.completed) params.set("completed", filters.completed);
    if (filters.priority) params.set("priority", filters.priority);
    if (cursor) params.set("cursor", cursor);
    return request<TodayPage>(
      `/tasks/today?${params}`,
      "GET",
      undefined,
      token,
      signal,
    );
  },
  createToday: (token: string, body: CreateTodayTask) =>
    request<Task>("/tasks/today", "POST", body, token),
  async tasks(
    token: string,
    filters: Filters,
    cursor = "",
    signal?: AbortSignal,
  ) {
    const params = new URLSearchParams({ limit: "20" });
    if (filters.planDate) params.set("plan_date", filters.planDate);
    if (filters.project) params.set("project_id", filters.project);
    if (filters.completed) params.set("completed", filters.completed);
    if (filters.priority) params.set("priority", filters.priority);
    if (cursor) params.set("cursor", cursor);
    let next = "";
    const items = await request<Task[]>(
      `/tasks?${params}`,
      "GET",
      undefined,
      token,
      signal,
      (response) => {
        next = response.headers.get("X-Next-Cursor") || "";
      },
    );
    return { items, next };
  },
  task: (token: string, id: string) =>
    request<Task>(`/tasks/${encodeURIComponent(id)}`, "GET", undefined, token),
  createTask: (token: string, body: CreateTask) =>
    request<Task>("/tasks", "POST", body, token),
  updateTask: (token: string, id: string, body: UpdateTask) =>
    request<Task>(`/tasks/${encodeURIComponent(id)}`, "PATCH", body, token),
  deleteTask: (token: string, id: string) =>
    request<void>(
      `/tasks/${encodeURIComponent(id)}`,
      "DELETE",
      undefined,
      token,
    ),
  projects: (token: string, signal?: AbortSignal) =>
    request<Project[]>("/projects", "GET", undefined, token, signal),
  createProject: (token: string, body: CreateProject) =>
    request<Project>("/projects", "POST", body, token),
  updateProject: (token: string, id: string, body: UpdateProject) =>
    request<Project>(
      `/projects/${encodeURIComponent(id)}`,
      "PATCH",
      body,
      token,
    ),
  deleteProject: (token: string, id: string) =>
    request<void>(
      `/projects/${encodeURIComponent(id)}`,
      "DELETE",
      undefined,
      token,
    ),
};
