import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { App } from "./App";
import { api, ApiError } from "./api/client";

const account = {
  id: "00000000-0000-4000-8000-000000000001",
  email: "learner@example.com",
  created_at: "2026-01-01T00:00:00Z",
};
const session = () => ({
  access_token: "test-memory-only",
  token_type: "Bearer" as const,
  expires_at: new Date(Date.now() + 3_600_000).toISOString(),
});
function mount(path = "/login") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}
async function fillLogin() {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("邮箱地址"), account.email);
  await user.type(
    screen.getByLabelText("密码", { exact: true }),
    "a long test passphrase",
  );
  await user.click(screen.getByRole("button", { name: /登录/ }));
  return user;
}
beforeEach(() => {
  vi.spyOn(api, "login").mockResolvedValue(session());
  vi.spyOn(api, "me").mockResolvedValue(account);
  vi.spyOn(api, "logout").mockResolvedValue(undefined);
  vi.spyOn(api, "register").mockResolvedValue(account);
});
it("guards profile and loses session on a full page remount without persisting credentials", async () => {
  const view = mount("/profile");
  expect(
    await screen.findByRole("heading", { name: "欢迎回来。" }),
  ).toBeVisible();
  await fillLogin();
  expect(
    await screen.findByRole("heading", { name: "你的个人空间" }),
  ).toBeVisible();
  expect(api.me).toHaveBeenCalledWith(
    "test-memory-only",
    expect.any(AbortSignal),
  );
  expect(localStorage.length).toBe(0);
  expect(sessionStorage.length).toBe(0);
  view.unmount();
  mount("/profile");
  expect(
    await screen.findByRole("heading", { name: "欢迎回来。" }),
  ).toBeVisible();
});
it("shows incorrect credentials without rendering a profile", async () => {
  vi.mocked(api.login).mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  mount();
  await fillLogin();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "邮箱或密码不正确",
  );
  expect(api.me).not.toHaveBeenCalled();
});
it("validates register, prevents duplicates while loading and does not auto-login", async () => {
  let finish!: (value: typeof account) => void;
  vi.mocked(api.register).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  const user = userEvent.setup();
  mount("/register");
  await user.click(screen.getByRole("button", { name: /创建账号/ }));
  expect(screen.getByLabelText("邮箱地址")).toHaveFocus();
  expect(api.register).not.toHaveBeenCalled();
  await user.type(screen.getByLabelText("邮箱地址"), account.email);
  await user.type(
    screen.getByLabelText("密码", { exact: true }),
    "a long test passphrase",
  );
  await user.type(screen.getByLabelText("确认密码"), "a long test passphrase");
  await user.dblClick(screen.getByRole("button", { name: /创建账号/ }));
  expect(api.register).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: /正在创建/ })).toBeDisabled();
  await act(async () => finish(account));
  expect(await screen.findByRole("status")).toHaveTextContent("注册成功");
  expect(api.login).not.toHaveBeenCalled();
  await user.click(screen.getByRole("link", { name: "前往登录" }));
  expect(screen.getByLabelText("密码", { exact: true })).toHaveValue("");
});
it("clears the session after a protected endpoint returns 401", async () => {
  mount();
  const user = await fillLogin();
  await screen.findByRole("heading", { name: "你的个人空间" });
  vi.mocked(api.me).mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  await user.click(screen.getByRole("button", { name: "刷新资料" }));
  expect(
    await screen.findByRole("heading", { name: "欢迎回来。" }),
  ).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent("登录已失效");
});
it("retains a retry path when logout fails, then clears on successful revocation", async () => {
  mount();
  const user = await fillLogin();
  await screen.findByRole("heading", { name: "你的个人空间" });
  vi.mocked(api.logout).mockRejectedValueOnce(new ApiError("NETWORK_ERROR"));
  await user.click(screen.getByRole("button", { name: "退出登录" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("退出未完成");
  expect(screen.getByRole("heading", { name: "你的个人空间" })).toBeVisible();
  await user.click(screen.getByRole("button", { name: "退出登录" }));
  expect(await screen.findByRole("status")).toHaveTextContent("已安全退出");
});
it("does not restore a session from a late login response after navigation", async () => {
  let finish!: (value: ReturnType<typeof session>) => void;
  vi.mocked(api.login).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  mount();
  const user = await fillLogin();
  await user.click(screen.getByRole("link", { name: "创建账号" }));
  await act(async () => finish(session()));
  expect(screen.getByRole("heading", { name: "开启你的空间" })).toBeVisible();
  await waitFor(() =>
    expect(api.logout).toHaveBeenCalledWith("test-memory-only"),
  );
});
it("shows a duplicate-email error on register", async () => {
  vi.mocked(api.register).mockRejectedValue(
    new ApiError("EMAIL_ALREADY_EXISTS", 409),
  );
  mount("/register");
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("邮箱地址"), account.email);
  await user.type(
    screen.getByLabelText("密码", { exact: true }),
    "a long test passphrase",
  );
  await user.type(screen.getByLabelText("确认密码"), "a long test passphrase");
  await user.click(screen.getByRole("button", { name: /创建账号/ }));
  expect(await screen.findByRole("alert")).toHaveTextContent("这个邮箱已注册");
});

it("expires the session locally without waiting for another API call", async () => {
  vi.useFakeTimers();
  try {
    vi.mocked(api.login).mockResolvedValue({
      ...session(),
      expires_at: new Date(Date.now() + 10_000).toISOString(),
    });
    mount();
    fireEvent.change(screen.getByLabelText("邮箱地址"), {
      target: { value: account.email },
    });
    fireEvent.change(screen.getByLabelText("密码", { exact: true }), {
      target: { value: "a long test passphrase" },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /登录/ }));
    });
    expect(screen.getByRole("heading", { name: "你的个人空间" })).toBeVisible();
    await act(async () => {
      vi.advanceTimersByTime(10_001);
    });
    expect(screen.getByRole("status")).toHaveTextContent("登录已过期");
  } finally {
    vi.useRealTimers();
  }
});
