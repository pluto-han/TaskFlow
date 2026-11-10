import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { DiscordSettings } from "../../../src/pages/DiscordSettings";
import { discordApi } from "../../../src/api/discord";
import { ApiError } from "../../../src/api/client";
const authorize = <T,>(
  action: (credentials: { csrfToken: string }) => Promise<T>,
) => action({ csrfToken: "test" });
vi.mock("../../../src/auth/AuthProvider", () => ({
  useAuth: () => ({ authorize }),
}));
beforeEach(() => {
  vi.spyOn(discordApi, "notifications").mockResolvedValue([]);
});
const empty = {
  enabled: true,
  bound: false,
  version: 1,
  guild_id: null,
  channel_id: null,
  channel_name: null,
  bound_at: null,
};

it("issues a private code in memory, refreshes binding and unbinds with a version", async () => {
  const status = vi.spyOn(discordApi, "status").mockResolvedValue(empty);
  vi.spyOn(discordApi, "issue").mockResolvedValue({
    code: "A".repeat(43),
    expires_at: new Date(Date.now() + 600000).toISOString(),
  });
  const unbind = vi.spyOn(discordApi, "unbind").mockResolvedValue(undefined);
  render(<DiscordSettings />);
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Generate connection code" }),
    ).toBeEnabled(),
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Generate connection code" }),
  );
  expect(
    await screen.findByRole("region", { name: "One-time connection code" }),
  ).toHaveTextContent("A".repeat(43));
  expect(Object.values(localStorage).join("")).not.toContain("A".repeat(43));
  status.mockResolvedValue({
    ...empty,
    bound: true,
    version: 2,
    guild_id: "111",
    channel_id: "222",
    channel_name: "test",
    bound_at: "2026-11-06T12:00:00Z",
  });
  fireEvent.click(screen.getByRole("button", { name: "Refresh connection" }));
  await screen.findByText("#test");
  expect(
    screen.queryByRole("region", { name: "One-time connection code" }),
  ).not.toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: "Disconnect and revoke codes" }),
  );
  expect(unbind).not.toHaveBeenCalled();
  status.mockResolvedValue({ ...empty, version: 3 });
  fireEvent.click(screen.getByRole("button", { name: "Confirm disconnect" }));
  await waitFor(() =>
    expect(unbind).toHaveBeenCalledWith({ csrfToken: "test" }, 2),
  );
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});

it("disables code issuance until configured and preserves the target on conflict", async () => {
  const status = vi
    .spyOn(discordApi, "status")
    .mockResolvedValue({ ...empty, enabled: false });
  vi.spyOn(discordApi, "unbind").mockRejectedValue(
    new ApiError("DISCORD_BINDING_CONFLICT", 409),
  );
  render(<DiscordSettings />);
  await screen.findByText(/Not set up yet/);
  expect(
    screen.getByText(/configure the Discord Application ID/),
  ).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Generate connection code" }),
  ).toBeDisabled();
  status.mockResolvedValue({
    ...empty,
    bound: true,
    version: 2,
    guild_id: "111",
    channel_id: "222",
    channel_name: "safe",
    bound_at: "2026-11-06T12:00:00Z",
  });
  fireEvent.click(screen.getByRole("button", { name: "Refresh connection" }));
  await screen.findByText("#safe");
  fireEvent.click(
    screen.getByRole("button", { name: "Disconnect and revoke codes" }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Confirm disconnect" }));
  const dialog = screen.getByRole("dialog");
  await waitFor(() =>
    expect(within(dialog).getByRole("alert")).toHaveTextContent(
      "The connection has changed",
    ),
  );
  expect(screen.getByText("#safe")).toBeVisible();
});

it("removes an expired code from the rendered settings", async () => {
  vi.useFakeTimers();
  try {
    vi.spyOn(discordApi, "status").mockResolvedValue(empty);
    vi.spyOn(discordApi, "issue").mockResolvedValue({
      code: "B".repeat(43),
      expires_at: new Date(Date.now() + 10000).toISOString(),
    });
    await act(async () => {
      render(<DiscordSettings />);
    });
    await act(async () => {
      fireEvent.click(
        screen.getByRole("button", { name: "Generate connection code" }),
      );
    });
    expect(
      screen.getByRole("region", { name: "One-time connection code" }),
    ).toBeVisible();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10001);
    });
    expect(
      screen.queryByRole("region", { name: "One-time connection code" }),
    ).not.toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});

it("shows recent notification results without displaying another user's data", async () => {
  vi.mocked(discordApi.notifications).mockResolvedValue([
    {
      id: "event",
      rule_id: "rule",
      task_id: "task",
      scheduled_date: "2026-11-06",
      title: "Reading",
      status: "retrying",
      attempts: 2,
      last_error: "network",
      created_at: "2026-11-06T12:00:00Z",
    },
  ]);
  vi.spyOn(discordApi, "status").mockResolvedValue(empty);
  render(<DiscordSettings />);
  expect(await screen.findByText("Reading")).toBeVisible();
  expect(screen.getByText("Waiting to retry")).toBeVisible();
});
