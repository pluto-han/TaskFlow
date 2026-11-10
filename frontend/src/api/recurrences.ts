import type { BrowserAuth } from "./types";
import { request } from "./client";
import type { components } from "./schema";
type Schemas = components["schemas"];
export type Recurrence = Schemas["Recurrence"];
export const recurrenceApi = {
  list: (credentials: BrowserAuth, cursor = "", signal?: AbortSignal) =>
    request<Schemas["RecurrencePage"]>(
      `/recurrences?limit=20&${cursor ? "cursor=" + encodeURIComponent(cursor) : ""}`,
      "GET",
      undefined,
      credentials,
      signal,
    ),
  create: (credentials: BrowserAuth, body: Schemas["CreateRecurrence"]) =>
    request<Recurrence>("/recurrences", "POST", body, credentials),
  update: (
    credentials: BrowserAuth,
    id: string,
    body: Schemas["UpdateRecurrence"],
  ) => request<Recurrence>(`/recurrences/${id}`, "PATCH", body, credentials),
  remove: (credentials: BrowserAuth, rule: Recurrence) =>
    request<void>(
      `/recurrences/${rule.id}`,
      "DELETE",
      { version: rule.version },
      credentials,
    ),
  preview: (
    credentials: BrowserAuth,
    anchor_date: string,
    interval_days: number,
    signal?: AbortSignal,
  ) =>
    request<Schemas["RecurrencePreview"]>(
      "/recurrences/preview",
      "POST",
      { anchor_date, interval_days },
      credentials,
      signal,
    ),
};
