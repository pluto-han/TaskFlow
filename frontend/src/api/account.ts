import { request } from "./client";
import type { BrowserAuth } from "./types";
import type { components } from "./schema";
type Schemas = components["schemas"];
export type SessionInfo = Schemas["SessionInfo"];
export type SessionPage = Schemas["SessionPage"];
export type EmailStatus = Schemas["EmailVerificationStatus"];
export const accountApi = {
  status: (auth: BrowserAuth, signal?: AbortSignal) =>
    request<EmailStatus>(
      "/auth/email/verification",
      "GET",
      undefined,
      auth,
      signal,
    ),
  verify: (auth: BrowserAuth, signal?: AbortSignal) =>
    request<void>("/auth/email/verification", "POST", undefined, auth, signal),
  sessions: (auth: BrowserAuth, cursor = "", signal?: AbortSignal) =>
    request<SessionPage>(
      `/auth/sessions?limit=10${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
      "GET",
      undefined,
      auth,
      signal,
    ),
  revoke: (auth: BrowserAuth, id: string, signal?: AbortSignal) =>
    request<void>(
      `/auth/sessions/${encodeURIComponent(id)}`,
      "DELETE",
      undefined,
      auth,
      signal,
    ),
  changePassword: (
    auth: BrowserAuth,
    input: Schemas["PasswordChangeRequest"],
    signal?: AbortSignal,
  ) => request<void>("/auth/password/change", "POST", input, auth, signal),
  requestReset: (email: string, signal?: AbortSignal) =>
    request<void>(
      "/auth/password/reset/request",
      "POST",
      { email },
      undefined,
      signal,
    ),
  reset: (token: string, new_password: string, signal?: AbortSignal) =>
    request<void>(
      "/auth/password/reset/confirm",
      "POST",
      { token, new_password },
      undefined,
      signal,
    ),
  confirmEmail: (token: string, signal?: AbortSignal) =>
    request<void>(
      "/auth/email/verification/confirm",
      "POST",
      { token },
      undefined,
      signal,
    ),
};
