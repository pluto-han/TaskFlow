import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { DiscordSettings } from "./DiscordSettings";
import { discordApi } from "../api/discord";
import { ApiError } from "../api/client";
const authorize = <T,>(action: (token: string) => Promise<T>) => action("test");
vi.mock("../auth/AuthProvider", () => ({ useAuth: () => ({ authorize }) }));
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
    expect(screen.getByRole("button", { name: "生成绑定码" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "生成绑定码" }));
  expect(
    await screen.findByRole("region", { name: "一次性绑定码" }),
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
  fireEvent.click(screen.getByRole("button", { name: "刷新绑定状态" }));
  await screen.findByText("#test");
  expect(
    screen.queryByRole("region", { name: "一次性绑定码" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "解绑并撤销绑定码" }));
  expect(unbind).not.toHaveBeenCalled();
  status.mockResolvedValue({ ...empty, version: 3 });
  fireEvent.click(screen.getByRole("button", { name: "确认解绑" }));
  await waitFor(() => expect(unbind).toHaveBeenCalledWith("test", 2));
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
  await screen.findByText(/服务尚未配置/);
  expect(
    screen.getByText(/请先配置 Discord Application ID 和公钥/),
  ).toBeVisible();
  expect(screen.getByRole("button", { name: "生成绑定码" })).toBeDisabled();
  status.mockResolvedValue({
    ...empty,
    bound: true,
    version: 2,
    guild_id: "111",
    channel_id: "222",
    channel_name: "safe",
    bound_at: "2026-11-06T12:00:00Z",
  });
  fireEvent.click(screen.getByRole("button", { name: "刷新绑定状态" }));
  await screen.findByText("#safe");
  fireEvent.click(screen.getByRole("button", { name: "解绑并撤销绑定码" }));
  fireEvent.click(screen.getByRole("button", { name: "确认解绑" }));
  const dialog = screen.getByRole("dialog");
  await waitFor(() =>
    expect(within(dialog).getByRole("alert")).toHaveTextContent("绑定已变化"),
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
      fireEvent.click(screen.getByRole("button", { name: "生成绑定码" }));
    });
    expect(screen.getByRole("region", { name: "一次性绑定码" })).toBeVisible();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10001);
    });
    expect(
      screen.queryByRole("region", { name: "一次性绑定码" }),
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
      title: "阅读",
      status: "retrying",
      attempts: 2,
      last_error: "network",
      created_at: "2026-11-06T12:00:00Z",
    },
  ]);
  vi.spyOn(discordApi, "status").mockResolvedValue(empty);
  render(<DiscordSettings />);
  expect(await screen.findByText("阅读")).toBeVisible();
  expect(screen.getByText("等待重试")).toBeVisible();
});
