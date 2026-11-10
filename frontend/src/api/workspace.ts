import type { BrowserAuth } from "./types";
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
  today(
    credentials: BrowserAuth,
    filters: Filters,
    cursor = "",
    signal?: AbortSignal,
  ) {
    const params = new URLSearchParams({ limit: "20" });
    if (filters.completed) params.set("completed", filters.completed);
    if (filters.priority) params.set("priority", filters.priority);
    if (cursor) params.set("cursor", cursor);
    return request<TodayPage>(
      `/tasks/today?${params}`,
      "GET",
      undefined,
      credentials,
      signal,
    );
  },
  createToday: (credentials: BrowserAuth, body: CreateTodayTask) =>
    request<Task>("/tasks/today", "POST", body, credentials),
  async tasks(
    credentials: BrowserAuth,
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
      credentials,
      signal,
      (response) => {
        next = response.headers.get("X-Next-Cursor") || "";
      },
    );
    return { items, next };
  },
  task: (credentials: BrowserAuth, id: string) =>
    request<Task>(
      `/tasks/${encodeURIComponent(id)}`,
      "GET",
      undefined,
      credentials,
    ),
  createTask: (credentials: BrowserAuth, body: CreateTask) =>
    request<Task>("/tasks", "POST", body, credentials),
  updateTask: (credentials: BrowserAuth, id: string, body: UpdateTask) =>
    request<Task>(
      `/tasks/${encodeURIComponent(id)}`,
      "PATCH",
      body,
      credentials,
    ),
  deleteTask: (credentials: BrowserAuth, id: string) =>
    request<void>(
      `/tasks/${encodeURIComponent(id)}`,
      "DELETE",
      undefined,
      credentials,
    ),
  projects: (credentials: BrowserAuth, signal?: AbortSignal) =>
    request<Project[]>("/projects", "GET", undefined, credentials, signal),
  createProject: (credentials: BrowserAuth, body: CreateProject) =>
    request<Project>("/projects", "POST", body, credentials),
  updateProject: (credentials: BrowserAuth, id: string, body: UpdateProject) =>
    request<Project>(
      `/projects/${encodeURIComponent(id)}`,
      "PATCH",
      body,
      credentials,
    ),
  deleteProject: (credentials: BrowserAuth, id: string) =>
    request<void>(
      `/projects/${encodeURIComponent(id)}`,
      "DELETE",
      undefined,
      credentials,
    ),
};
