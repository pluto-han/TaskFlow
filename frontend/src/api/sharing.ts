import { request } from "./client";
import type { Member, Invitation } from "./types";
const path = (id: string) => `/projects/${encodeURIComponent(id)}`;
export const sharingApi = {
  members: (token: string, id: string, signal?: AbortSignal) =>
    request<Member[]>(`${path(id)}/members`, "GET", undefined, token, signal),
  invitations: (token: string, id: string, signal?: AbortSignal) =>
    request<Invitation[]>(
      `${path(id)}/invitations`,
      "GET",
      undefined,
      token,
      signal,
    ),
  invite: (token: string, id: string, userID: string) =>
    request<Invitation>(
      `${path(id)}/invitations`,
      "POST",
      { user_id: userID },
      token,
    ),
  revoke: (token: string, id: string, iid: string) =>
    request<void>(
      `${path(id)}/invitations/${encodeURIComponent(iid)}`,
      "DELETE",
      undefined,
      token,
    ),
  remove: (token: string, id: string, userID: string) =>
    request<void>(
      `${path(id)}/members/${encodeURIComponent(userID)}`,
      "DELETE",
      undefined,
      token,
    ),
  leave: (token: string, id: string) =>
    request<void>(`${path(id)}/leave`, "POST", undefined, token),
  transfer: (token: string, id: string, userID: string) =>
    request<void>(`${path(id)}/ownership`, "POST", { user_id: userID }, token),
  accept: (token: string, invitation: string) =>
    request<{ project_id: string }>(
      "/invitations/accept",
      "POST",
      { token: invitation },
      token,
    ),
};
