import type { BrowserAuth } from "./types";
import { request } from "./client";
import type { Member, Invitation } from "./types";
const path = (id: string) => `/projects/${encodeURIComponent(id)}`;
export const sharingApi = {
  members: (credentials: BrowserAuth, id: string, signal?: AbortSignal) =>
    request<Member[]>(
      `${path(id)}/members`,
      "GET",
      undefined,
      credentials,
      signal,
    ),
  invitations: (credentials: BrowserAuth, id: string, signal?: AbortSignal) =>
    request<Invitation[]>(
      `${path(id)}/invitations`,
      "GET",
      undefined,
      credentials,
      signal,
    ),
  invite: (credentials: BrowserAuth, id: string, userID: string) =>
    request<Invitation>(
      `${path(id)}/invitations`,
      "POST",
      { user_id: userID },
      credentials,
    ),
  revoke: (credentials: BrowserAuth, id: string, iid: string) =>
    request<void>(
      `${path(id)}/invitations/${encodeURIComponent(iid)}`,
      "DELETE",
      undefined,
      credentials,
    ),
  remove: (credentials: BrowserAuth, id: string, userID: string) =>
    request<void>(
      `${path(id)}/members/${encodeURIComponent(userID)}`,
      "DELETE",
      undefined,
      credentials,
    ),
  leave: (credentials: BrowserAuth, id: string) =>
    request<void>(`${path(id)}/leave`, "POST", undefined, credentials),
  transfer: (credentials: BrowserAuth, id: string, userID: string) =>
    request<void>(
      `${path(id)}/ownership`,
      "POST",
      { user_id: userID },
      credentials,
    ),
  accept: (credentials: BrowserAuth, invitation: string) =>
    request<{ project_id: string }>(
      "/invitations/accept",
      "POST",
      { token: invitation },
      credentials,
    ),
};
