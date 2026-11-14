import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { TaskDetails } from "../../../src/tasks/TaskDetails";
import { TaskBoard } from "../../../src/tasks/TaskBoard";
import { workspaceApi } from "../../../src/api/workspace";
import { ApiError } from "../../../src/api/client";
import type { Task } from "../../../src/api/types";

const authorize = <T,>(
  action: (credentials: { csrfToken: string }) => Promise<T>,
) => action({ csrfToken: "test" });
vi.mock("../../../src/auth/AuthProvider", () => ({
  useAuth: () => ({ authorize }),
}));
const task: Task = {
  can_move: true,
  plan_date: null,
  recurrence_id: null,
  id: "one",
  title: "Task one",
  description: "",
  completed: false,
  priority: 2,
  due_date: null,
  project_id: null,
  version: 1,
  created_at: "2026-01-01T00:00:00Z",
};
const filters = { project: "", completed: "", priority: "" };
const board = (project = "") => (
  <TaskBoard
    key={project}
    filters={project ? { ...filters, project } : filters}
    projects={[]}
    title="All tasks"
  />
);
beforeEach(() => {
  vi.spyOn(workspaceApi, "tasks").mockResolvedValue({
    items: [task],
    next: "",
  });
  vi.spyOn(workspaceApi, "updateTask").mockResolvedValue({
    ...task,
    completed: true,
    version: 2,
  });
});
it("optimistically toggles and rolls back on a version conflict", async () => {
  let reject!: (err: Error) => void;
  vi.mocked(workspaceApi.updateTask).mockReturnValue(
    new Promise((_, no) => {
      reject = no;
    }),
  );
  render(board());
  const checkbox = await screen.findByRole("checkbox");
  fireEvent.click(checkbox);
  expect(checkbox).toBeChecked();
  expect(checkbox).toBeDisabled();
  await act(async () => reject(new ApiError("TASK_CONFLICT", 409)));
  expect(checkbox).not.toBeChecked();
  expect(screen.getByRole("alert")).toHaveTextContent(
    "The completion change was undone",
  );
  expect(workspaceApi.updateTask).toHaveBeenCalledTimes(1);
});
it("loads the next cursor, deduplicates rows, and resets on a filter switch", async () => {
  vi.mocked(workspaceApi.tasks)
    .mockResolvedValueOnce({ items: [task], next: "cursor-one" })
    .mockResolvedValueOnce({
      items: [task, { ...task, id: "two", title: "Task two" }],
      next: "",
    })
    .mockResolvedValueOnce({ items: [], next: "" });
  const view = render(board());
  fireEvent.click(await screen.findByRole("button", { name: "Load more" }));
  expect(await screen.findByText("Task two")).toBeVisible();
  expect(screen.getAllByRole("checkbox")).toHaveLength(2);
  expect(workspaceApi.tasks).toHaveBeenLastCalledWith(
    { csrfToken: "test" },
    filters,
    "cursor-one",
    expect.any(AbortSignal),
  );
  view.rerender(board("none"));
  expect(await screen.findByText("No tasks")).toBeVisible();
  expect(screen.queryByText("Task one")).not.toBeInTheDocument();
  expect(workspaceApi.tasks).toHaveBeenLastCalledWith(
    { csrfToken: "test" },
    { ...filters, project: "none" },
    "",
    expect.any(AbortSignal),
  );
});
it("ignores a late response from the old filter", async () => {
  let resolve!: (page: { items: Task[]; next: string }) => void;
  vi.mocked(workspaceApi.tasks)
    .mockReturnValueOnce(
      new Promise((yes) => {
        resolve = yes;
      }),
    )
    .mockResolvedValueOnce({ items: [], next: "" });
  const view = render(board());
  view.rerender(board("none"));
  await screen.findByText("No tasks");
  await act(async () => resolve({ items: [task], next: "old" }));
  expect(screen.queryByText("Task one")).not.toBeInTheDocument();
});
it("offers a retry after a list failure", async () => {
  vi.mocked(workspaceApi.tasks).mockRejectedValueOnce(
    new ApiError("NETWORK_ERROR"),
  );
  render(board());
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "Refresh list" }));
  await waitFor(() => expect(screen.getByText("Task one")).toBeVisible());
});

it("keeps the confirmed new version even when the following refresh fails", async () => {
  vi.mocked(workspaceApi.tasks)
    .mockResolvedValueOnce({ items: [task], next: "" })
    .mockRejectedValue(new ApiError("NETWORK_ERROR"));
  render(board());
  fireEvent.click(await screen.findByRole("checkbox"));
  await screen.findByRole("alert");
  const checkbox = screen.getByRole("checkbox");
  expect(checkbox).toBeChecked();
  await waitFor(() => expect(checkbox).toBeEnabled());
  fireEvent.click(checkbox);
  await waitFor(() =>
    expect(workspaceApi.updateTask).toHaveBeenLastCalledWith(
      { csrfToken: "test" },
      task.id,
      {
        version: 2,
        completed: false,
      },
    ),
  );
});

const details = (id: string, loaded?: Task, revision = 0) => (
  <TaskDetails
    key={id}
    id={id}
    task={loaded}
    revision={revision}
    projects={[]}
    edit={vi.fn()}
    remove={vi.fn()}
    close={vi.fn()}
  />
);
it("uses a listed task without fetching it again", async () => {
  const fetch = vi.spyOn(workspaceApi, "task");
  render(details(task.id, task));
  expect(screen.getByRole("heading", { name: task.title })).toHaveFocus();
  expect(fetch).not.toHaveBeenCalled();
});
it("loads a deep-linked task that is outside the current page", async () => {
  vi.spyOn(workspaceApi, "task").mockResolvedValue(task);
  render(details(task.id));
  expect(
    await screen.findByRole("heading", { name: task.title }),
  ).toBeVisible();
  expect(workspaceApi.task).toHaveBeenCalledWith(
    { csrfToken: "test" },
    task.id,
    expect.any(AbortSignal),
  );
});
it("aborts old details and ignores their late response after selecting another task", async () => {
  let resolve!: (value: Task) => void;
  const fetch = vi
    .spyOn(workspaceApi, "task")
    .mockReturnValueOnce(
      new Promise((yes) => {
        resolve = yes;
      }),
    )
    .mockResolvedValue({ ...task, id: "two", title: "Second task" });
  const view = render(details(task.id));
  const signal = fetch.mock.calls[0][2]!;
  view.rerender(details("two"));
  expect(signal.aborted).toBe(true);
  await screen.findByRole("heading", { name: "Second task" });
  await act(async () => resolve(task));
  expect(
    screen.queryByRole("heading", { name: task.title }),
  ).not.toBeInTheDocument();
});
it.each([403, 404])(
  "clears inaccessible details on a %s response without selecting a fallback",
  async (status) => {
    vi.spyOn(workspaceApi, "task")
      .mockResolvedValueOnce(task)
      .mockRejectedValue(new ApiError("TASK_NOT_FOUND", status));
    const view = render(details(task.id));
    await screen.findByRole("heading", { name: task.title });
    view.rerender(details(task.id, undefined, 1));
    await screen.findByRole("alert");
    expect(
      screen.queryByRole("heading", { name: task.title }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: `Edit task: ${task.title}` }),
    ).not.toBeInTheDocument();
  },
);
it("clears the project list and selection when project access is revoked", async () => {
  const lost = vi.fn(),
    select = vi.fn();
  vi.mocked(workspaceApi.tasks)
    .mockResolvedValueOnce({ items: [task], next: "" })
    .mockRejectedValue(new ApiError("PROJECT_NOT_FOUND", 404));
  render(
    <TaskBoard
      filters={{ ...filters, project: "p" }}
      projects={[]}
      title="Project"
      onAccessLost={lost}
      selectTask={select}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(lost).toHaveBeenCalledTimes(1));
  expect(select).toHaveBeenCalledWith("");
  expect(screen.queryByText(task.title)).not.toBeInTheDocument();
});
