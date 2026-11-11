import {
  act,
  fireEvent,
  render,
  screen,
  within,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { App } from "../../src/App";
import { api, ApiError } from "../../src/api/client";
import { accountApi } from "../../src/api/account";
import { discordApi } from "../../src/api/discord";
import { captureLink, type AccountLink } from "../../src/auth/capability";

const token = "T".repeat(43);
const session = {
  user: {
    id: "user",
    email: "learner@example.com",
    created_at: "2026-01-01T00:00:00Z",
  },
  csrf_token: "C".repeat(43),
  expires_at: "2099-01-01T00:00:00Z",
};
const current = {
  id: "current",
  current: true,
  credential_kind: "browser" as const,
  created_at: "2026-11-10T10:00:00Z",
  expires_at: "2099-01-01T00:00:00Z",
};
const other = {
  ...current,
  id: "other",
  current: false,
  created_at: "2026-11-09T10:00:00Z",
};
function mount(path = "/profile", accountLink?: AccountLink) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App accountLink={accountLink} />
    </MemoryRouter>,
  );
}
beforeEach(() => {
  vi.spyOn(api, "session").mockResolvedValue(session);
  vi.spyOn(api, "logout").mockResolvedValue(undefined);
  vi.spyOn(api, "login").mockResolvedValue(session);
  vi.spyOn(accountApi, "status").mockResolvedValue({
    enabled: true,
    verified_at: null,
  });
  vi.spyOn(accountApi, "sessions").mockResolvedValue({
    items: [current, other],
    next_cursor: "",
  });
  vi.spyOn(accountApi, "verify").mockResolvedValue(undefined);
  vi.spyOn(accountApi, "revoke").mockResolvedValue(undefined);
  vi.spyOn(accountApi, "changePassword").mockResolvedValue(undefined);
  vi.spyOn(accountApi, "requestReset").mockResolvedValue(undefined);
  vi.spyOn(accountApi, "reset").mockResolvedValue(undefined);
  vi.spyOn(accountApi, "confirmEmail").mockResolvedValue(undefined);
  vi.spyOn(discordApi, "status").mockResolvedValue({
    enabled: false,
    bound: false,
    version: 1,
    guild_id: null,
    channel_id: null,
    channel_name: null,
    bound_at: null,
  });
  vi.spyOn(discordApi, "notifications").mockResolvedValue([]);
});
it("shows disabled email honestly without blocking password or sessions", async () => {
  vi.mocked(accountApi.status).mockResolvedValue({
    enabled: false,
    verified_at: null,
  });
  mount();
  expect(
    await screen.findByText(/Email delivery is not available/),
  ).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Send verification email" }),
  ).not.toBeInTheDocument();
  expect(await screen.findByText("Current session")).toBeVisible();
  expect(screen.getByText("Change password")).toBeVisible();
});
it("requests verification once and refreshes verification state", async () => {
  const user = userEvent.setup();
  mount();
  await user.click(
    await screen.findByRole("button", { name: "Send verification email" }),
  );
  expect(
    await screen.findByRole("button", { name: "Email requested" }),
  ).toBeDisabled();
  expect(accountApi.verify).toHaveBeenCalledTimes(1);
  expect(accountApi.verify).toHaveBeenCalledWith(
    { csrfToken: session.csrf_token },
    expect.any(AbortSignal),
  );
  vi.mocked(accountApi.status).mockResolvedValue({
    enabled: true,
    verified_at: "2026-11-11T10:00:00Z",
  });
  await user.click(
    screen.getByRole("button", { name: "Refresh email status" }),
  );
  expect(await screen.findByText("Verified")).toBeVisible();
});
it("requires confirmation before revoking another session", async () => {
  const user = userEvent.setup();
  mount();
  await user.click(
    await screen.findByRole("button", { name: /End session signed in/ }),
  );
  const dialog = screen.getByRole("dialog");
  expect(accountApi.revoke).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(accountApi.revoke).not.toHaveBeenCalled();
  await user.click(
    screen.getByRole("button", { name: /End session signed in/ }),
  );
  await user.click(screen.getByRole("button", { name: "Confirm sign out" }));
  await waitFor(() =>
    expect(accountApi.revoke).toHaveBeenCalledWith(
      { csrfToken: session.csrf_token },
      "other",
      expect.any(AbortSignal),
    ),
  );
  expect(await screen.findByText("Session signed out.")).toBeVisible();
  expect(api.logout).not.toHaveBeenCalled();
});
it("ends the local session after current-session revocation", async () => {
  vi.spyOn(api, "session")
    .mockResolvedValueOnce(session)
    .mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  const user = userEvent.setup();
  mount();
  await user.click(
    await screen.findByRole("button", { name: "End current session" }),
  );
  await user.click(screen.getByRole("button", { name: "Confirm sign out" }));
  expect(
    await screen.findByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  expect(
    screen.getByText("This session has ended. Sign in again to continue."),
  ).toBeVisible();
});
it("validates passwords, preserves fields on failure and clears sign-in on success", async () => {
  const user = userEvent.setup();
  mount();
  await user.click(await screen.findByText("Change password"));
  await user.click(screen.getByRole("button", { name: "Update password" }));
  expect(screen.getByLabelText("Current password")).toHaveFocus();
  await user.type(
    screen.getByLabelText("Current password"),
    "current password",
  );
  await user.type(
    screen.getByLabelText("New password", { exact: true }),
    "a new password",
  );
  await user.type(
    screen.getByLabelText("Confirm new password"),
    "a new password",
  );
  vi.mocked(accountApi.changePassword).mockRejectedValueOnce(
    new ApiError("CURRENT_PASSWORD_INCORRECT", 400),
  );
  await user.click(screen.getByRole("button", { name: "Update password" }));
  expect(
    await screen.findByText(/Your current password is incorrect/),
  ).toBeVisible();
  expect(screen.getByLabelText("New password", { exact: true })).toHaveValue(
    "a new password",
  );
  await user.click(screen.getByRole("button", { name: "Update password" }));
  expect(
    await screen.findByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
  expect(screen.getByText(/Your password has been changed/)).toBeVisible();
});
it("keeps recovery available during an unresolved session bootstrap", async () => {
  vi.mocked(api.session).mockImplementation(() => new Promise(() => {}));
  mount("/forgot-password");
  expect(
    await screen.findByRole("heading", { name: "Forgot your password?" }),
  ).toBeVisible();
});
it("shows neutral acceptance and handles disabled mail with a way back", async () => {
  const user = userEvent.setup();
  mount("/forgot-password");
  await user.type(
    screen.getByLabelText("Email address"),
    "unknown@example.com",
  );
  await user.click(screen.getByRole("button", { name: "Send reset link" }));
  expect(
    await screen.findByRole("heading", { name: "Check your email" }),
  ).toBeVisible();
  expect(screen.getByRole("status")).toHaveTextContent(
    "If this address belongs to an account",
  );
  await user.click(
    screen.getByRole("link", { name: "Try another email address" }),
  );
  vi.mocked(accountApi.requestReset).mockRejectedValue(
    new ApiError("EMAIL_DISABLED", 503),
  );
  await user.click(screen.getByRole("button", { name: "Send reset link" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Email delivery is not available",
  );
  expect(
    screen.getByRole("button", { name: "Send reset link" }),
  ).toBeDisabled();
});
it("does not consume a verification link on load and consumes only on explicit submit", async () => {
  const user = userEvent.setup();
  mount("/verify-email", { kind: "verify", token });
  expect(
    await screen.findByRole("heading", { name: "Verify your email" }),
  ).toBeVisible();
  expect(accountApi.confirmEmail).not.toHaveBeenCalled();
  await user.click(
    screen.getByRole("button", { name: "Verify email address" }),
  );
  expect(
    await screen.findByRole("heading", { name: "Email verified" }),
  ).toBeVisible();
  expect(accountApi.confirmEmail).toHaveBeenCalledWith(
    token,
    expect.any(AbortSignal),
  );
  expect(document.body.textContent).not.toContain(token);
});
it("retains reset inputs on network failure and explains invalid links", async () => {
  const user = userEvent.setup();
  mount("/reset-password", { kind: "reset", token });
  await user.type(
    screen.getByLabelText("New password", { exact: true }),
    "a new password",
  );
  await user.type(
    screen.getByLabelText("Confirm new password"),
    "a new password",
  );
  vi.mocked(accountApi.reset).mockRejectedValueOnce(
    new ApiError("NETWORK_ERROR"),
  );
  await user.click(screen.getByRole("button", { name: "Save new password" }));
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByLabelText("New password", { exact: true })).toHaveValue(
    "a new password",
  );
  vi.mocked(accountApi.reset).mockRejectedValueOnce(
    new ApiError("RESET_TOKEN_INVALID", 400),
  );
  await user.click(screen.getByRole("button", { name: "Save new password" }));
  expect(
    await screen.findByRole("heading", {
      name: "This link is no longer available",
    }),
  ).toBeVisible();
  expect(
    screen.getByRole("link", { name: "Request a new link" }),
  ).toHaveAttribute("href", "/forgot-password");
  expect(accountApi.reset).toHaveBeenCalledTimes(2);
});
it("removes fragments before requests and rejects query-string and duplicate capabilities", () => {
  for (const hash of [
    `#token=${token}`,
    `#token=${token}&token=${token}`,
    "#token=invalid",
  ]) {
    window.history.replaceState(
      null,
      "",
      `/reset-password?token=untrusted${hash}`,
    );
    const result = captureLink(window.location, window.history);
    expect(window.location.hash).toBe("");
    expect(window.location.search).toBe("");
    expect(result.accountLink?.token).toBe(
      hash === `#token=${token}` ? token : "",
    );
  }
  expect(localStorage.length + sessionStorage.length).toBe(0);
  window.history.replaceState(null, "", "/");
});

it("keeps existing sessions visible when loading the next page fails", async () => {
  const user = userEvent.setup();
  vi.mocked(accountApi.sessions)
    .mockResolvedValueOnce({ items: [current], next_cursor: "next-page" })
    .mockRejectedValueOnce(new ApiError("NETWORK_ERROR"));
  mount();
  await user.click(
    await screen.findByRole("button", { name: "Load more sessions" }),
  );
  expect(await screen.findByRole("alert")).toBeVisible();
  expect(screen.getByText("Current session")).toBeVisible();
  expect(accountApi.sessions).toHaveBeenLastCalledWith(
    { csrfToken: session.csrf_token },
    "next-page",
    expect.any(AbortSignal),
  );
});
it("does not automatically retry verification after CSRF rejection", async () => {
  const user = userEvent.setup();
  vi.mocked(accountApi.verify).mockRejectedValue(
    new ApiError("CSRF_INVALID", 403),
  );
  mount();
  await user.click(
    await screen.findByRole("button", { name: "Send verification email" }),
  );
  await screen.findByRole("heading", { name: "Sign-in & security" });
  expect(accountApi.verify).toHaveBeenCalledTimes(1);
});
it("does not sign in or sign out an unrelated current account after resetting a capability", async () => {
  const user = userEvent.setup();
  mount("/reset-password", { kind: "reset", token });
  await user.type(
    screen.getByLabelText("New password", { exact: true }),
    "a new password",
  );
  await user.type(
    screen.getByLabelText("Confirm new password"),
    "a new password",
  );
  await user.click(screen.getByRole("button", { name: "Save new password" }));
  expect(
    await screen.findByRole("heading", { name: "Password updated" }),
  ).toBeVisible();
  expect(api.login).not.toHaveBeenCalled();
  expect(api.logout).not.toHaveBeenCalled();
  expect(screen.getByRole("link", { name: "Tasks" })).toBeVisible();
});

it("enables an explicit resend after the cooldown without sending automatically", async () => {
  const view = mount();
  const button = await screen.findByRole("button", {
    name: "Send verification email",
  });
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  try {
    await act(async () => {
      fireEvent.click(button);
    });
    expect(
      screen.getByRole("button", { name: "Email requested" }),
    ).toBeDisabled();
    act(() => vi.advanceTimersByTime(60_000));
    expect(
      screen.getByRole("button", { name: "Resend verification email" }),
    ).toBeEnabled();
    expect(accountApi.verify).toHaveBeenCalledTimes(1);
  } finally {
    vi.useRealTimers();
    view.unmount();
  }
});
