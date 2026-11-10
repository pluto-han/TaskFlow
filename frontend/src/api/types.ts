import type { components } from "./schema";

export type User = components["schemas"]["User"];
export type LoginRequest = components["schemas"]["LoginRequest"];
export type RegisterRequest = components["schemas"]["RegisterRequest"];
export type BrowserSession = components["schemas"]["BrowserSession"];
export type BrowserAuth = Readonly<{ csrfToken: string }>;
export type ErrorResponse = components["schemas"]["Error"];
export type Task = components["schemas"]["Task"];
export type CreateTask = components["schemas"]["CreateTask"];
export type UpdateTask = components["schemas"]["UpdateTask"];
export type Project = components["schemas"]["Project"];
export type CreateProject = components["schemas"]["CreateProject"];
export type UpdateProject = components["schemas"]["UpdateProject"];
export type Member = components["schemas"]["Member"];
export type Invitation = components["schemas"]["Invitation"];

export type TodayPage = components["schemas"]["TodayPage"];
export type CreateTodayTask = components["schemas"]["CreateTodayTask"];
