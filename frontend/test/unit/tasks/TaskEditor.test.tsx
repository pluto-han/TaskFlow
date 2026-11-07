import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { TaskEditor } from "../../../src/tasks/TaskEditor";
import { workspaceApi } from "../../../src/api/workspace";
import { ApiError } from "../../../src/api/client";
import type { Task } from "../../../src/api/types";

vi.mock("../../../src/auth/AuthProvider", () => ({
  useAuth: () => ({
    authorize: <T,>(action: (token: string) => Promise<T>) => action("test"),
  }),
}));
const task: Task = {
  can_move: true,
  plan_date: null,
  recurrence_id: null,
  id: "task1",
  title: "Read",
  description: "",
  priority: 2,
  completed: false,
  project_id: null,
  due_date: "2026-07-15T12:30:20.123Z",
  created_at: "2026-01-01T00:00:00Z",
  version: 4,
};
beforeEach(() => {
  vi.spyOn(workspaceApi, "updateTask").mockResolvedValue({
    ...task,
    version: 5,
  });
  vi.spyOn(workspaceApi, "task").mockResolvedValue({
    ...task,
    title: "Changed elsewhere",
    version: 5,
  });
});
function open() {
  render(
    <TaskEditor
      task={task}
      projects={[]}
      defaultProject=""
      close={vi.fn()}
      saved={vi.fn()}
    />,
  );
}
it("omits an unchanged due date, retaining original precision", async () => {
  open();
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Task title"), " more");
  await user.click(screen.getByRole("button", { name: "Save task" }));
  expect(workspaceApi.updateTask).toHaveBeenCalledWith("test", "task1", {
    version: 4,
    title: "Read more",
  });
});
it("sends explicit null when clearing the due date", async () => {
  open();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Clear due date" }));
  await user.click(screen.getByRole("button", { name: "Save task" }));
  expect(workspaceApi.updateTask).toHaveBeenCalledWith("test", "task1", {
    version: 4,
    due_date: null,
  });
});
it("sends UTC for a changed local input", async () => {
  open();
  fireEvent.change(screen.getByLabelText("Due date (local time)"), {
    target: { value: "2026-12-15T10:30" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save task" }));
  await waitFor(() =>
    expect(workspaceApi.updateTask).toHaveBeenCalledWith("test", "task1", {
      version: 4,
      due_date: new Date(2026, 11, 15, 10, 30).toISOString(),
    }),
  );
});
it("keeps the draft on 409 and never silently retries with a new version", async () => {
  vi.mocked(workspaceApi.updateTask).mockRejectedValueOnce(
    new ApiError("TASK_CONFLICT", 409),
  );
  open();
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Task title"), " my draft");
  await user.click(screen.getByRole("button", { name: "Save task" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This task has changed",
  );
  expect(screen.getByLabelText("Task title")).toHaveValue("Read my draft");
  expect(screen.getByRole("button", { name: "Save task" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Load latest version" }));
  expect(await screen.findByText("Changed elsewhere")).toBeVisible();
  expect(screen.getByLabelText("Task title")).toHaveValue("Read my draft");
  expect(workspaceApi.updateTask).toHaveBeenCalledTimes(1);
  await user.click(
    screen.getByRole("button", {
      name: "Discard draft and use latest version",
    }),
  );
  expect(screen.getByLabelText("Task title")).toHaveValue("Changed elsewhere");
  await user.type(screen.getByLabelText("Task title"), " resolved");
  await user.click(screen.getByRole("button", { name: "Save task" }));
  expect(workspaceApi.updateTask).toHaveBeenLastCalledWith("test", "task1", {
    version: 5,
    title: "Changed elsewhere resolved",
  });
});

it("disables moving a task created by someone else while permitting edits", async () => {
  render(
    <TaskEditor
      task={{ ...task, can_move: false }}
      projects={[]}
      defaultProject=""
      close={vi.fn()}
      saved={vi.fn()}
    />,
  );
  expect(screen.getByLabelText("Project")).toBeDisabled();
  expect(screen.getByLabelText("Task title")).toBeEnabled();
});
