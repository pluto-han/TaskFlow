import type { BrowserAuth } from "./types";
import { request } from "./client";
import type { components } from "./schema";
type Schemas = components["schemas"];
export const discordApi = {
  notifications: (credentials: BrowserAuth, signal?: AbortSignal) =>
    request<Schemas["NotificationEvent"][]>(
      "/integrations/discord/notifications",
      "GET",
      undefined,
      credentials,
      signal,
    ),
  status: (credentials: BrowserAuth, signal?: AbortSignal) =>
    request<Schemas["DiscordBinding"]>(
      "/integrations/discord",
      "GET",
      undefined,
      credentials,
      signal,
    ),
  issue: (credentials: BrowserAuth) =>
    request<Schemas["DiscordBindingCode"]>(
      "/integrations/discord/code",
      "POST",
      undefined,
      credentials,
    ),
  unbind: (credentials: BrowserAuth, version: number) =>
    request<void>("/integrations/discord", "DELETE", { version }, credentials),
};
