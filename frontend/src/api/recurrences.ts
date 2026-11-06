import { request } from "./client";
import type { components } from "./schema";
type Schemas = components["schemas"];
export type Recurrence = Schemas["Recurrence"];
export const recurrenceApi = {
  list: (token: string, cursor = "", signal?: AbortSignal) =>
    request<Schemas["RecurrencePage"]>(
      `/recurrences?limit=20&${cursor ? "cursor=" + encodeURIComponent(cursor) : ""}`,
      "GET",
      undefined,
      token,
      signal,
    ),
  create: (token: string, body: Schemas["CreateRecurrence"]) =>
    request<Recurrence>("/recurrences", "POST", body, token),
  update: (token: string, id: string, body: Schemas["UpdateRecurrence"]) =>
    request<Recurrence>(`/recurrences/${id}`, "PATCH", body, token),
  remove: (token: string, rule: Recurrence) =>
    request<void>(
      `/recurrences/${rule.id}`,
      "DELETE",
      { version: rule.version },
      token,
    ),
  preview: (
    token: string,
    anchor_date: string,
    interval_days: number,
    signal?: AbortSignal,
  ) =>
    request<Schemas["RecurrencePreview"]>(
      "/recurrences/preview",
      "POST",
      { anchor_date, interval_days },
      token,
      signal,
    ),
};
