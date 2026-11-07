import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { TaskBoard } from "../../../src/tasks/TaskBoard";
import { TaskEditor } from "../../../src/tasks/TaskEditor";
import { WorkspacePage } from "../../../src/pages/WorkspacePage";
import { workspaceApi } from "../../../src/api/workspace";
import { ApiError } from "../../../src/api/client";
import type { Task, TodayPage } from "../../../src/api/types";
const authorize = <T,>(action: (token: string) => Promise<T>) => action("test");
vi.mock("../../../src/auth/AuthProvider", () => ({
  useAuth: () => ({ authorize, user: { id: "user" } }),
}));
const item: Task = {
  id: "today-one",
  title: "Today’s reading",
  description: "",
  priority: 2,
  version: 1,
  completed: false,
  created_at: "2026-11-05T10:00:00Z",
  due_date: null,
  project_id: null,
  plan_date: "2026-11-05",
  recurrence_id: null,
  can_move: false,
};
const filters = { project: "", completed: "", priority: "" };
const page = (overrides: Partial<TodayPage> = {}): TodayPage => ({
  items: [item],
  next_cursor: "",
  plan_date: "2026-11-05",
  timezone: "Europe/Amsterdam",
  server_time: "2026-11-05T10:00:00Z",
  next_day_at: "2026-11-05T23:00:00Z",
  ...overrides,
});
const board = () => (
  <TaskBoard
    today
    filters={filters}
    projects={[]}
    title="Today"
    setMutating={vi.fn()}
  />
);
afterEach(() => {
  vi.useRealTimers();
});

it("uses server day, excludes project inputs and preserves a draft across midnight", async () => {
  vi.spyOn(workspaceApi, "today")
    .mockResolvedValueOnce(page())
    .mockResolvedValue(page({ plan_date: "2026-11-06", items: [] }));
  render(board());
  await screen.findByText(item.title);
  fireEvent.click(screen.getByRole("button", { name: "＋ New task" }));
  fireEvent.change(screen.getByLabelText("Task title"), {
    target: { value: "Keep this draft" },
  });
  expect(screen.queryByLabelText("Project")).not.toBeInTheDocument();
  fireEvent(window, new Event("focus"));
  await waitFor(() =>
    expect(
      screen.getByRole("region", { name: "Planning date changed" }),
    ).toBeVisible(),
  );
  expect(screen.getByLabelText("Task title")).toHaveValue("Keep this draft");
  expect(screen.getByRole("button", { name: "Save task" })).toBeDisabled();
  expect(
    screen.queryByRole("heading", { name: item.title }),
  ).not.toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: "Refresh and confirm date" }),
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Save task" })).toBeEnabled(),
  );
});

it("handles PLAN_DAY_CHANGED separately from version conflict and never auto-submits", async () => {
  const create = vi
    .spyOn(workspaceApi, "createToday")
    .mockRejectedValueOnce(new ApiError("PLAN_DAY_CHANGED", 409))
    .mockResolvedValue(item);
  const refresh = vi.fn().mockResolvedValue("2026-11-06");
  const saved = vi.fn();
  const editor = (date: string) => (
    <TaskEditor
      todayDate={date}
      refreshToday={refresh}
      projects={[]}
      defaultProject=""
      close={vi.fn()}
      saved={saved}
    />
  );
  const view = render(editor("2026-11-05"));
  fireEvent.change(screen.getByLabelText("Task title"), {
    target: { value: "My draft" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save task" }));
  await screen.findByRole("region", { name: "Planning date changed" });
  expect(
    screen.queryByRole("button", { name: "Load latest version" }),
  ).not.toBeInTheDocument();
  expect(screen.getByLabelText("Task title")).toHaveValue("My draft");
  view.rerender(editor("2026-11-06"));
  fireEvent.click(
    screen.getByRole("button", { name: "Refresh and confirm date" }),
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Save task" })).toBeEnabled(),
  );
  expect(create).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Save task" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(create).toHaveBeenLastCalledWith("test", {
    plan_date: "2026-11-06",
    title: "My draft",
    description: "",
    priority: 2,
    due_date: null,
  });
});

it("restarts pagination when yesterday's cursor is rejected", async () => {
  const today = vi
    .spyOn(workspaceApi, "today")
    .mockResolvedValueOnce(page({ next_cursor: "yesterday" }))
    .mockRejectedValueOnce(new ApiError("INVALID_QUERY", 400))
    .mockResolvedValue(page({ plan_date: "2026-11-06", items: [] }));
  render(board());
  fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
  await screen.findByText("No tasks here yet");
  expect(today.mock.calls.map((c) => c[2])).toEqual(["", "yesterday", ""]);
  expect(screen.queryByText(item.title)).not.toBeInTheDocument();
});

it("aborts late pages when the tab becomes visible or reconnects", async () => {
  let resolve!: (p: TodayPage) => void;
  const today = vi
    .spyOn(workspaceApi, "today")
    .mockResolvedValueOnce(page({ next_cursor: "next" }))
    .mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    )
    .mockResolvedValue(page({ plan_date: "2026-11-06", items: [] }));
  render(board());
  fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
  fireEvent(window, new Event("online"));
  await screen.findByText("No tasks here yet");
  await act(async () =>
    resolve(
      page({
        items: [{ ...item, id: "late", title: "Yesterday’s late response" }],
      }),
    ),
  );
  expect(
    screen.queryByText("Yesterday’s late response"),
  ).not.toBeInTheDocument();
  expect(today.mock.calls[1][3]?.aborted).toBe(true);
  fireEvent(document, new Event("visibilitychange"));
  await waitFor(() => expect(today).toHaveBeenCalledTimes(4));
});

it.each([
  ["2026-03-28T23:00:00Z", "2026-03-29T22:00:00Z", 23],
  ["2026-10-24T22:00:00Z", "2026-10-25T23:00:00Z", 25],
])(
  "refreshes at the server midnight after %s, including DST",
  async (server_time, next_day_at, hours) => {
    vi.useFakeTimers();
    const today = vi
      .spyOn(workspaceApi, "today")
      .mockResolvedValue(page({ server_time, next_day_at }));
    const view = render(board());
    await act(async () => {
      await Promise.resolve();
    });
    expect(today).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(Number(hours) * 3600_000 - 100);
    });
    expect(today).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(today).toHaveBeenCalledTimes(2);
    view.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(26 * 3600_000);
    });
    expect(today).toHaveBeenCalledTimes(2);
  },
);

it("discards a response whose server midnight elapsed in transit", async () => {
  vi.useFakeTimers();
  let resolve!: (p: TodayPage) => void;
  vi.spyOn(workspaceApi, "today")
    .mockReturnValueOnce(
      new Promise((r) => {
        resolve = r;
      }),
    )
    .mockResolvedValue(page({ plan_date: "2026-11-06", items: [] }));
  const view = render(board());
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
    resolve(page({ server_time: "2026-11-05T22:59:59Z" }));
  });
  expect(screen.queryByText(item.title)).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "＋ New task" })).toBeDisabled();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(screen.getByText("No tasks here yet")).toBeVisible();
  view.unmount();
});

it("shows recovery for an unauthorized/failed today request without allowing a new task", async () => {
  vi.spyOn(workspaceApi, "today")
    .mockRejectedValueOnce(new ApiError("UNAUTHORIZED", 401))
    .mockResolvedValue(page());
  render(board());
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "＋ New task" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Refresh list" }));
  await screen.findByText(item.title);
});

it("switches navigation, hides project creation in today and filters history only in all tasks", async () => {
  vi.spyOn(workspaceApi, "projects").mockResolvedValue([]);
  const tasks = vi
    .spyOn(workspaceApi, "tasks")
    .mockResolvedValue({ items: [item], next: "" });
  vi.spyOn(workspaceApi, "today").mockResolvedValue(page());
  render(
    <MemoryRouter>
      <WorkspacePage />
    </MemoryRouter>,
  );
  await screen.findByText(item.title);
  fireEvent.change(screen.getByLabelText("Filter by planned date"), {
    target: { value: "2026-11-04" },
  });
  await waitFor(() =>
    expect(tasks).toHaveBeenLastCalledWith(
      "test",
      expect.objectContaining({ planDate: "2026-11-04" }),
      "",
      expect.any(AbortSignal),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Today" }));
  await screen.findByText(item.title);
  expect(
    screen.queryByRole("button", { name: "New project" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByLabelText("Filter by planned date"),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /All tasks/ }));
  expect(screen.getByRole("button", { name: "New project" })).toBeVisible();
  expect(screen.getByLabelText("Filter by planned date")).toHaveValue("");
});

it("preserves the draft when refreshing the plan date fails", async () => {
  const refresh = vi.fn().mockResolvedValue(null);
  const view = render(
    <TaskEditor
      todayDate="2026-11-05"
      refreshToday={refresh}
      projects={[]}
      defaultProject=""
      close={vi.fn()}
      saved={vi.fn()}
    />,
  );
  fireEvent.change(screen.getByLabelText("Task title"), {
    target: { value: "Keep this through a lost connection" },
  });
  view.rerender(
    <TaskEditor
      todayDate="2026-11-06"
      refreshToday={refresh}
      projects={[]}
      defaultProject=""
      close={vi.fn()}
      saved={vi.fn()}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Refresh and confirm date" }),
  );
  await screen.findByRole("alert");
  expect(screen.getByLabelText("Task title")).toHaveValue(
    "Keep this through a lost connection",
  );
  expect(screen.getByRole("button", { name: "Save task" })).toBeDisabled();
});

it("hides project movement for historical plans without claiming the owner is someone else", () => {
  render(
    <TaskEditor
      task={item}
      projects={[]}
      defaultProject=""
      close={vi.fn()}
      saved={vi.fn()}
    />,
  );
  expect(screen.queryByLabelText("Project")).not.toBeInTheDocument();
  expect(
    screen.queryByText(/Only the person who created this task/),
  ).not.toBeInTheDocument();
  expect(
    screen.getByText(/Personal plans stay outside projects/),
  ).toBeVisible();
});

it("does not restart polling when an in-flight mutation finishes after leaving the view", async () => {
  const today = vi.spyOn(workspaceApi, "today").mockResolvedValue(page());
  let resolve!: (task: Task) => void;
  vi.spyOn(workspaceApi, "updateTask").mockReturnValue(
    new Promise((r) => {
      resolve = r;
    }),
  );
  const view = render(board());
  fireEvent.click(await screen.findByRole("checkbox"));
  view.unmount();
  await act(async () => resolve({ ...item, completed: true, version: 2 }));
  expect(today).toHaveBeenCalledTimes(1);
});
