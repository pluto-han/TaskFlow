import { request } from "./client";
import type { components } from "./schema";
type Schemas = components["schemas"];
export const discordApi = {
  notifications: (token: string, signal?: AbortSignal) =>
    request<Schemas["NotificationEvent"][]>(
      "/integrations/discord/notifications",
      "GET",
      undefined,
      token,
      signal,
    ),
  status: (token: string, signal?: AbortSignal) =>
    request<Schemas["DiscordBinding"]>(
      "/integrations/discord",
      "GET",
      undefined,
      token,
      signal,
    ),
  issue: (token: string) =>
    request<Schemas["DiscordBindingCode"]>(
      "/integrations/discord/code",
      "POST",
      undefined,
      token,
    ),
  unbind: (token: string, version: number) =>
    request<void>("/integrations/discord", "DELETE", { version }, token),
};
