import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
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
    setMutating={vi.fn()}
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
  expect(await screen.findByText("No tasks here yet")).toBeVisible();
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
  await screen.findByText("No tasks here yet");
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
