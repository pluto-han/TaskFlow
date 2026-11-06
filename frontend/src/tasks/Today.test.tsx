import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { TaskBoard } from "./TaskBoard";
import { TaskEditor } from "./TaskEditor";
import { WorkspacePage } from "../pages/WorkspacePage";
import { workspaceApi } from "../api/workspace";
import { ApiError } from "../api/client";
import type { Task, TodayPage } from "../api/types";
const authorize = <T,>(action: (token: string) => Promise<T>) => action("test");
vi.mock("../auth/AuthProvider", () => ({
  useAuth: () => ({ authorize, user: { id: "user" } }),
}));
const item: Task = {
  id: "today-one",
  title: "今日阅读",
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
    title="今日任务"
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
  fireEvent.click(screen.getByRole("button", { name: "＋ 新建任务" }));
  fireEvent.change(screen.getByLabelText("任务标题"), {
    target: { value: "保留这份草稿" },
  });
  expect(screen.queryByLabelText("所属项目")).not.toBeInTheDocument();
  fireEvent(window, new Event("focus"));
  await waitFor(() =>
    expect(screen.getByRole("region", { name: "计划日期变化" })).toBeVisible(),
  );
  expect(screen.getByLabelText("任务标题")).toHaveValue("保留这份草稿");
  expect(screen.getByRole("button", { name: "保存任务" })).toBeDisabled();
  expect(
    screen.queryByRole("heading", { name: item.title }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "刷新并确认计划日期" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存任务" })).toBeEnabled(),
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
  fireEvent.change(screen.getByLabelText("任务标题"), {
    target: { value: "我的草稿" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存任务" }));
  await screen.findByRole("region", { name: "计划日期变化" });
  expect(
    screen.queryByRole("button", { name: "读取最新版本" }),
  ).not.toBeInTheDocument();
  expect(screen.getByLabelText("任务标题")).toHaveValue("我的草稿");
  view.rerender(editor("2026-11-06"));
  fireEvent.click(screen.getByRole("button", { name: "刷新并确认计划日期" }));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "保存任务" })).toBeEnabled(),
  );
  expect(create).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "保存任务" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(create).toHaveBeenLastCalledWith("test", {
    plan_date: "2026-11-06",
    title: "我的草稿",
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
  fireEvent.click(await screen.findByRole("button", { name: "加载更多" }));
  await screen.findByText("这里还没有任务");
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
  fireEvent.click(await screen.findByRole("button", { name: "加载更多" }));
  fireEvent(window, new Event("online"));
  await screen.findByText("这里还没有任务");
  await act(async () =>
    resolve(
      page({ items: [{ ...item, id: "late", title: "昨天的迟到响应" }] }),
    ),
  );
  expect(screen.queryByText("昨天的迟到响应")).not.toBeInTheDocument();
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
  expect(screen.getByRole("button", { name: "＋ 新建任务" })).toBeDisabled();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1000);
  });
  expect(screen.getByText("这里还没有任务")).toBeVisible();
  view.unmount();
});

it("shows recovery for an unauthorized/failed today request without allowing a new task", async () => {
  vi.spyOn(workspaceApi, "today")
    .mockRejectedValueOnce(new ApiError("UNAUTHORIZED", 401))
    .mockResolvedValue(page());
  render(board());
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "＋ 新建任务" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "刷新列表" }));
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
  fireEvent.change(screen.getByLabelText("筛选计划日期"), {
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
  fireEvent.click(screen.getByRole("button", { name: "今日任务" }));
  await screen.findByText(item.title);
  expect(
    screen.queryByRole("button", { name: "新建项目" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByLabelText("筛选计划日期")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /全部任务/ }));
  expect(screen.getByRole("button", { name: "新建项目" })).toBeVisible();
  expect(screen.getByLabelText("筛选计划日期")).toHaveValue("");
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
  fireEvent.change(screen.getByLabelText("任务标题"), {
    target: { value: "断网也保留" },
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
  fireEvent.click(screen.getByRole("button", { name: "刷新并确认计划日期" }));
  await screen.findByRole("alert");
  expect(screen.getByLabelText("任务标题")).toHaveValue("断网也保留");
  expect(screen.getByRole("button", { name: "保存任务" })).toBeDisabled();
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
  expect(screen.queryByLabelText("所属项目")).not.toBeInTheDocument();
  expect(screen.queryByText(/你不是这项任务的创建者/)).not.toBeInTheDocument();
  expect(screen.getByText(/个人计划不能加入项目/)).toBeVisible();
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
