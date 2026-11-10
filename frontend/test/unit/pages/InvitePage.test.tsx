import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { InvitePage } from "../../../src/pages/InvitePage";
import { sharingApi } from "../../../src/api/sharing";
import { ApiError } from "../../../src/api/client";
const state = vi.hoisted(() => ({ loggedIn: true }));
vi.mock("../../../src/auth/AuthProvider", () => ({
  useAuth: () => ({
    user: state.loggedIn ? { email: "recipient@example.com" } : null,
    authorize: <T,>(
      action: (credentials: { csrfToken: string }) => Promise<T>,
    ) => action({ csrfToken: "session" }),
  }),
}));
beforeEach(() => {
  state.loggedIn = true;
  vi.spyOn(sharingApi, "accept").mockResolvedValue({ project_id: "project" });
});
it("requires explicit acceptance and clears the in-memory invitation after success", async () => {
  const consumed = vi.fn();
  render(
    <MemoryRouter>
      <InvitePage token="invitation-secret" consumed={consumed} />
    </MemoryRouter>,
  );
  expect(sharingApi.accept).not.toHaveBeenCalled();
  await userEvent.click(
    screen.getByRole("button", { name: "Accept invitation" }),
  );
  expect(await screen.findByRole("status")).toHaveTextContent(
    "You are part of the project now",
  );
  expect(sharingApi.accept).toHaveBeenCalledWith(
    { csrfToken: "session" },
    "invitation-secret",
  );
  expect(consumed).toHaveBeenCalledOnce();
  expect(localStorage.length + sessionStorage.length).toBe(0);
});
it("keeps the invitation in memory while directing unauthenticated users to login", () => {
  state.loggedIn = false;
  render(
    <MemoryRouter>
      <InvitePage token="secret" consumed={vi.fn()} />
    </MemoryRouter>,
  );
  expect(
    screen.getByRole("link", { name: "Sign in to accept" }),
  ).toHaveAttribute("href", "/login?next=invite");
  expect(screen.queryByText("secret")).not.toBeInTheDocument();
});
it("shows a generic failure for used/revoked/wrong-recipient tokens", async () => {
  vi.mocked(sharingApi.accept).mockRejectedValue(
    new ApiError("INVITATION_INVALID", 400),
  );
  const consumed = vi.fn();
  render(
    <MemoryRouter>
      <InvitePage token="secret" consumed={consumed} />
    </MemoryRouter>,
  );
  await userEvent.click(
    screen.getByRole("button", { name: "Accept invitation" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This invitation has expired",
  );
  expect(consumed).not.toHaveBeenCalled();
});
