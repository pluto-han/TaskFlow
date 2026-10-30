import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { TaskEditor } from "./TaskEditor";
import { workspaceApi } from "../api/workspace";
import { ApiError } from "../api/client";
import type { Task } from "../api/types";

vi.mock("../auth/AuthProvider", () => ({
  useAuth: () => ({
    authorize: <T,>(action: (token: string) => Promise<T>) => action("test"),
  }),
}));
const task: Task = {
  can_move: true,
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
  await user.type(screen.getByLabelText("任务标题"), " more");
  await user.click(screen.getByRole("button", { name: "保存任务" }));
  expect(workspaceApi.updateTask).toHaveBeenCalledWith("test", "task1", {
    version: 4,
    title: "Read more",
  });
});
it("sends explicit null when clearing the due date", async () => {
  open();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "清除截止日期" }));
  await user.click(screen.getByRole("button", { name: "保存任务" }));
  expect(workspaceApi.updateTask).toHaveBeenCalledWith("test", "task1", {
    version: 4,
    due_date: null,
  });
});
it("sends UTC for a changed local input", async () => {
  open();
  fireEvent.change(screen.getByLabelText("截止日期（本地时间）"), {
    target: { value: "2026-12-15T10:30" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存任务" }));
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
  await user.type(screen.getByLabelText("任务标题"), " my draft");
  await user.click(screen.getByRole("button", { name: "保存任务" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("任务已被修改");
  expect(screen.getByLabelText("任务标题")).toHaveValue("Read my draft");
  expect(screen.getByRole("button", { name: "保存任务" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "读取最新版本" }));
  expect(await screen.findByText("Changed elsewhere")).toBeVisible();
  expect(screen.getByLabelText("任务标题")).toHaveValue("Read my draft");
  expect(workspaceApi.updateTask).toHaveBeenCalledTimes(1);
  await user.click(
    screen.getByRole("button", { name: "放弃当前输入，使用最新内容" }),
  );
  expect(screen.getByLabelText("任务标题")).toHaveValue("Changed elsewhere");
  await user.type(screen.getByLabelText("任务标题"), " resolved");
  await user.click(screen.getByRole("button", { name: "保存任务" }));
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
  expect(screen.getByLabelText("所属项目")).toBeDisabled();
  expect(screen.getByLabelText("任务标题")).toBeEnabled();
});
