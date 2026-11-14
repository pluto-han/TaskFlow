import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation, useNavigate } from "react-router";
import { beforeEach, expect, it, vi } from "vitest";
import { App } from "../../src/App";
import { api, ApiError } from "../../src/api/client";
import { workspaceApi } from "../../src/api/workspace";
import { sharingApi } from "../../src/api/sharing";
import { accountApi } from "../../src/api/account";
import { discordApi } from "../../src/api/discord";
import { returnTarget } from "../../src/auth/navigation";

const session = {
  user: {
    id: "u",
    email: "route@example.com",
    created_at: "2026-01-01T00:00:00Z",
  },
  csrf_token: "C".repeat(43),
  expires_at: "2099-01-01T00:00:00Z",
};
function History() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <output aria-label="Current route">
        {location.pathname + location.search}
      </output>
      <button onClick={() => navigate(-1)}>Back</button>
    </>
  );
}
function mount(path: string, invitationToken = "") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <History />
      <App invitationToken={invitationToken} />
    </MemoryRouter>,
  );
}
beforeEach(() => {
  vi.spyOn(api, "session").mockResolvedValue(session);
  vi.spyOn(api, "login").mockResolvedValue(session);
  vi.spyOn(workspaceApi, "projects").mockResolvedValue([
    {
      id: "p",
      name: "Project",
      description: "",
      role: "owner",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  ]);
  vi.spyOn(workspaceApi, "tasks").mockResolvedValue({ items: [], next: "" });
  vi.spyOn(workspaceApi, "today").mockResolvedValue({
    items: [],
    next_cursor: "",
    plan_date: "2026-11-14",
    timezone: "Europe/Amsterdam",
    server_time: "2026-11-14T10:00:00Z",
    next_day_at: "2026-11-14T23:00:00Z",
  });
  vi.spyOn(accountApi, "status").mockResolvedValue({
    enabled: false,
    verified_at: null,
  });
  vi.spyOn(accountApi, "sessions").mockResolvedValue({
    items: [],
    next_cursor: "",
  });
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
async function signIn() {
  const user = userEvent.setup();
  await user.type(
    await screen.findByLabelText("Email address"),
    session.user.email,
  );
  await user.type(
    screen.getByLabelText("Password", { exact: true }),
    "a long test passphrase",
  );
  await user.click(screen.getByRole("button", { name: "Sign in" }));
}
it("defaults a regular login to tasks without mounting profile services", async () => {
  vi.mocked(api.session).mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  mount("/login");
  await signIn();
  await screen.findByRole("heading", { name: "All tasks" });
  expect(screen.getByLabelText("Current route")).toHaveTextContent("/tasks");
  expect(accountApi.sessions).not.toHaveBeenCalled();
  expect(discordApi.status).not.toHaveBeenCalled();
});
it("preserves a protected deep link through login", async () => {
  vi.mocked(api.session).mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  mount("/tasks/today?priority=3&completed=false");
  await signIn();
  await screen.findByRole("heading", { name: "Today" });
  expect(screen.getByLabelText("Current route")).toHaveTextContent(
    "/tasks/today?priority=3&completed=false",
  );
  expect(workspaceApi.today).toHaveBeenCalledWith(
    expect.anything(),
    expect.objectContaining({ priority: "3", completed: "false" }),
    "",
    expect.any(AbortSignal),
  );
});
it("loads project routes directly and keeps unassigned separate", async () => {
  mount("/projects/p/tasks?priority=2");
  await screen.findByRole("heading", { name: "Project" });
  await waitFor(() =>
    expect(workspaceApi.tasks).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ project: "p", priority: "2" }),
      "",
      expect.any(AbortSignal),
    ),
  );
  await userEvent.click(screen.getByRole("link", { name: "Unassigned" }));
  await screen.findByRole("heading", { name: "Unassigned" });
  expect(screen.getByLabelText("Current route")).toHaveTextContent(
    "/tasks/unassigned?priority=2",
  );
  expect(workspaceApi.tasks).toHaveBeenLastCalledWith(
    expect.anything(),
    expect.objectContaining({ project: "none" }),
    "",
    expect.any(AbortSignal),
  );
});
it("restores filters and views through browser history and settings", async () => {
  mount("/tasks?priority=1");
  await screen.findByRole("heading", { name: "All tasks" });
  await userEvent.click(screen.getByRole("link", { name: "Today" }));
  await screen.findByRole("heading", { name: "Today" });
  await userEvent.click(screen.getByRole("link", { name: "Settings" }));
  await screen.findByRole("heading", { name: "Account" });
  await userEvent.click(screen.getByRole("link", { name: "Security" }));
  expect(
    await screen.findByRole("heading", { name: "Security" }),
  ).toBeVisible();
  expect(screen.getByRole("link", { name: "Settings" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await userEvent.click(screen.getByRole("link", { name: "Tasks" }));
  await screen.findByRole("heading", { name: "Today" });
  expect(screen.getByLabelText("Filter by priority")).toHaveValue("1");
});
it("back restores a prior filter and resets the task page", async () => {
  mount("/tasks?priority=1");
  await screen.findByRole("heading", { name: "All tasks" });
  await userEvent.selectOptions(
    screen.getByLabelText("Filter by priority"),
    "3",
  );
  await waitFor(() =>
    expect(screen.getByLabelText("Current route")).toHaveTextContent(
      "priority=3",
    ),
  );
  await userEvent.click(screen.getByRole("button", { name: "Back" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Filter by priority")).toHaveValue("1"),
  );
  expect(workspaceApi.tasks).toHaveBeenLastCalledWith(
    expect.anything(),
    expect.objectContaining({ priority: "1" }),
    "",
    expect.any(AbortSignal),
  );
});
it("removes invalid and duplicate filters and rejects impossible calendar dates", async () => {
  mount("/tasks?completed=maybe&priority=1&priority=3&plan_date=2026-02-30");
  await waitFor(() =>
    expect(screen.getByLabelText("Current route").textContent).toBe("/tasks"),
  );
  expect(screen.getByLabelText("Filter by planned date")).toHaveValue("");
});
it("keeps the profile compatibility redirect", async () => {
  mount("/profile");
  await screen.findByRole("heading", { name: "Account" });
  expect(screen.getByLabelText("Current route")).toHaveTextContent(
    "/settings/account",
  );
});
it("invitation login takes precedence without putting its capability in the URL", async () => {
  vi.mocked(api.session).mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  const token = "T".repeat(43);
  mount("/login?next=invite&returnTo=%2Ftasks%2Ftoday", token);
  await signIn();
  await screen.findByRole("button", { name: "Accept invitation" });
  expect(screen.getByLabelText("Current route").textContent).toBe("/invite");
  expect(screen.getByLabelText("Current route")).not.toHaveTextContent(token);
});
it.each([
  "https://evil.example/tasks",
  "//evil.example/tasks",
  "//[",
  "/\\evil.example/tasks",
  "/reset-password?token=secret",
  "/tasks#token=secret",
  "/tasks?token=secret",
  "/login",
  "/unknown",
])("rejects unsafe return target %s", (value) => {
  expect(returnTarget(value)).toBe("/tasks");
});
it("keeps account recovery usable when session restoration fails", async () => {
  vi.mocked(api.session).mockRejectedValue(new ApiError("NETWORK_ERROR"));
  mount("/forgot-password");
  await act(async () => {});
  expect(
    screen.getByRole("heading", { name: "Forgot your password?" }),
  ).toBeVisible();
});

it("keeps project settings in the current project and returns to its filters", async () => {
  vi.spyOn(sharingApi, "members").mockResolvedValue([]);
  vi.spyOn(sharingApi, "invitations").mockResolvedValue([]);
  mount("/projects/p/tasks?priority=3&completed=false");
  await userEvent.click(
    await screen.findByRole("link", { name: "Project settings" }),
  );
  await screen.findByRole("heading", { name: "Members & invitations" });
  const target = "/projects/p/tasks?priority=3&completed=false";
  expect(screen.getByRole("link", { name: "Tasks" })).toHaveAttribute(
    "href",
    target,
  );
  expect(screen.getByRole("link", { name: "Back to tasks" })).toHaveAttribute(
    "href",
    target,
  );
  expect(screen.getByRole("link", { name: "Project" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await userEvent.click(screen.getByRole("link", { name: "Back to tasks" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Current route").textContent).toBe(target),
  );
  expect(screen.getByLabelText("Filter by priority")).toHaveValue("3");
});

it("closes details when a saved task no longer matches the current filter", async () => {
  const task = {
    id: "selected",
    title: "Review",
    description: "",
    completed: false,
    priority: 1,
    project_id: null,
    due_date: null,
    plan_date: null,
    recurrence_id: null,
    can_move: true,
    version: 1,
    created_at: "2026-11-14T10:00:00Z",
  };
  vi.mocked(workspaceApi.tasks)
    .mockResolvedValueOnce({ items: [task], next: "" })
    .mockResolvedValue({ items: [], next: "" });
  vi.spyOn(workspaceApi, "task").mockResolvedValue(task);
  vi.spyOn(workspaceApi, "updateTask").mockResolvedValue({
    ...task,
    priority: 3,
    version: 2,
  });
  mount("/tasks?priority=1&task=selected");
  const user = userEvent.setup();
  await user.click(
    await screen.findByRole("button", { name: "Edit task: Review" }),
  );
  await user.selectOptions(
    within(screen.getByRole("dialog")).getByLabelText("Priority", {
      exact: true,
    }),
    "3",
  );
  await user.click(screen.getByRole("button", { name: "Save task" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Current route").textContent).toBe(
      "/tasks?priority=1",
    ),
  );
  expect(
    screen.queryByRole("complementary", { name: "Task details" }),
  ).not.toBeInTheDocument();
  expect(await screen.findByText("No tasks")).toBeVisible();
});
