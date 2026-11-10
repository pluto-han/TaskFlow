import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { App } from "../../src/App";
import { discordApi } from "../../src/api/discord";
import { api, ApiError } from "../../src/api/client";

const account = {
  id: "00000000-0000-4000-8000-000000000001",
  email: "learner@example.com",
  created_at: "2026-01-01T00:00:00Z",
};
const session = () => ({
  user: account,
  csrf_token: "C".repeat(43),
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
  await screen.findByLabelText("Email address");
  await user.type(screen.getByLabelText("Email address"), account.email);
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "a long test passphrase",
  );
  await user.click(screen.getByRole("button", { name: /Sign in/ }));
  return user;
}
beforeEach(() => {
  vi.spyOn(discordApi, "notifications").mockResolvedValue([]);
  vi.spyOn(discordApi, "status").mockResolvedValue({
    enabled: false,
    bound: false,
    version: 1,
    guild_id: null,
    channel_id: null,
    channel_name: null,
    bound_at: null,
  });
  vi.spyOn(api, "login").mockResolvedValue(session());
  vi.spyOn(api, "session").mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  vi.spyOn(api, "logout").mockResolvedValue(undefined);
  vi.spyOn(api, "register").mockResolvedValue(account);
});
it("restores a valid session on remount without persisting credentials", async () => {
  const view = mount("/profile");
  expect(
    await screen.findByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  await fillLogin();
  expect(
    await screen.findByRole("heading", { name: "A space of your own" }),
  ).toBeVisible();
  expect(api.login).toHaveBeenCalledWith(
    { email: account.email, password: "a long test passphrase" },
    expect.any(AbortSignal),
  );
  expect(localStorage.length).toBe(0);
  expect(sessionStorage.length).toBe(0);
  view.unmount();
  vi.mocked(api.session).mockResolvedValue(session());
  mount("/profile");
  expect(
    await screen.findByRole("heading", { name: "A space of your own" }),
  ).toBeVisible();
});
it("shows incorrect credentials without rendering a profile", async () => {
  vi.mocked(api.login).mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  mount();
  await fillLogin();
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "The email or password is incorrect",
  );
  expect(api.session).toHaveBeenCalledTimes(1);
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
  await screen.findByLabelText("Email address");
  await user.click(screen.getByRole("button", { name: /Create account/ }));
  expect(screen.getByLabelText("Email address")).toHaveFocus();
  expect(api.register).not.toHaveBeenCalled();
  await user.type(screen.getByLabelText("Email address"), account.email);
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "a long test passphrase",
  );
  await user.type(
    screen.getByLabelText("Confirm password"),
    "a long test passphrase",
  );
  await user.dblClick(screen.getByRole("button", { name: /Create account/ }));
  expect(api.register).toHaveBeenCalledTimes(1);
  expect(
    screen.getByRole("button", { name: /Creating your space/ }),
  ).toBeDisabled();
  await act(async () => finish(account));
  expect(await screen.findByRole("status")).toHaveTextContent(
    "Your space is ready",
  );
  expect(api.login).not.toHaveBeenCalled();
  await user.click(screen.getByRole("link", { name: "Continue to sign in" }));
  expect(screen.getByLabelText("Password", { exact: true })).toHaveValue("");
});
it("clears the session after a protected endpoint returns 401", async () => {
  mount();
  const user = await fillLogin();
  await screen.findByRole("heading", { name: "A space of your own" });
  vi.mocked(api.session).mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  await user.click(screen.getByRole("button", { name: "Refresh profile" }));
  expect(
    await screen.findByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Your session has ended",
  );
});
it("retains a retry path when logout fails, then clears on successful revocation", async () => {
  mount();
  const user = await fillLogin();
  await screen.findByRole("heading", { name: "A space of your own" });
  vi.mocked(api.logout).mockRejectedValueOnce(new ApiError("NETWORK_ERROR"));
  await user.click(screen.getByRole("button", { name: "Sign out" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "We could not sign you out",
  );
  expect(
    screen.getByRole("heading", { name: "A space of your own" }),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Sign out" }));
  expect(await screen.findByRole("status")).toHaveTextContent(
    "You have signed out safely",
  );
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
  await user.click(screen.getByRole("link", { name: "Create account" }));
  await act(async () => finish(session()));
  expect(
    screen.getByRole("heading", { name: "Make yourself a little space" }),
  ).toBeVisible();
  expect(api.logout).not.toHaveBeenCalled();
});
it("shows a duplicate-email error on register", async () => {
  vi.mocked(api.register).mockRejectedValue(
    new ApiError("EMAIL_ALREADY_EXISTS", 409),
  );
  mount("/register");
  await screen.findByLabelText("Email address");
  const user = userEvent.setup();
  await screen.findByLabelText("Email address");
  await user.type(screen.getByLabelText("Email address"), account.email);
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "a long test passphrase",
  );
  await user.type(
    screen.getByLabelText("Confirm password"),
    "a long test passphrase",
  );
  await user.click(screen.getByRole("button", { name: /Create account/ }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "An account already uses this email",
  );
});

it("expires the session locally without waiting for another API call", async () => {
  vi.useFakeTimers();
  try {
    vi.mocked(api.login).mockResolvedValue({
      ...session(),
      expires_at: new Date(Date.now() + 10_000).toISOString(),
    });
    mount();
    await act(async () => {});
    fireEvent.change(screen.getByLabelText("Email address"), {
      target: { value: account.email },
    });
    fireEvent.change(screen.getByLabelText("Password", { exact: true }), {
      target: { value: "a long test passphrase" },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Sign in/ }));
    });
    expect(
      screen.getByRole("heading", { name: "A space of your own" }),
    ).toBeVisible();
    await act(async () => {
      vi.advanceTimersByTime(10_001);
    });
    expect(screen.getByRole("status")).toHaveTextContent(
      "Your session has expired",
    );
  } finally {
    vi.useRealTimers();
  }
});

it("clears authentication when a task request returns 401", async () => {
  const { workspaceApi } = await import("../../src/api/workspace");
  vi.spyOn(workspaceApi, "projects").mockResolvedValue([]);
  vi.spyOn(workspaceApi, "tasks").mockRejectedValue(
    new ApiError("UNAUTHORIZED", 401),
  );
  mount();
  const user = await fillLogin();
  await screen.findByRole("heading", { name: "A space of your own" });
  await user.click(screen.getByRole("link", { name: /^Tasks$/ }));
  expect(
    await screen.findByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent(
    "Your session has ended",
  );
});
