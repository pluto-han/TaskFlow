import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { RecurrenceBoard, RecurrenceEditor } from "../../../src/tasks/RecurrenceBoard";
import { MemoryRouter } from "react-router";
import { discordApi } from "../../../src/api/discord";
import { recurrenceApi, type Recurrence } from "../../../src/api/recurrences";
import { ApiError } from "../../../src/api/client";
const authorize = <T,>(action: (token: string) => Promise<T>) => action("test");
vi.mock("../../../src/auth/AuthProvider", () => ({ useAuth: () => ({ authorize }) }));
const rule: Recurrence = {
  reminder_enabled: false,
  id: "r",
  title: "Read",
  description: "",
  priority: 2,
  anchor_date: "2026-11-05",
  interval_days: 3,
  next_run_date: "2026-11-05",
  paused: false,
  version: 1,
  created_at: "2026-11-05T12:00:00Z",
  updated_at: "2026-11-05T12:00:00Z",
  preview_dates: ["2026-11-05", "2026-11-08", "2026-11-11"],
};
beforeEach(() =>
  vi.spyOn(discordApi, "status").mockResolvedValue({
    enabled: true,
    bound: true,
    version: 1,
    guild_id: "111",
    channel_id: "222",
    channel_name: "test",
    bound_at: "2026-11-06T12:00:00Z",
  }),
);
const preview = () =>
  vi.spyOn(recurrenceApi, "preview").mockResolvedValue({
    plan_date: "2026-11-05",
    timezone: "Europe/Amsterdam",
    dates: rule.preview_dates,
  });

it("defaults to server day, previews and creates a rule without task or project fields", async () => {
  preview();
  const create = vi.spyOn(recurrenceApi, "create").mockResolvedValue(rule);
  const saved = vi.fn();
  render(
    <MemoryRouter>
      <RecurrenceEditor day="2026-11-05" close={vi.fn()} saved={saved} />
    </MemoryRouter>,
  );
  expect(screen.getByLabelText("First date")).toHaveValue("2026-11-05");
  fireEvent.change(screen.getByLabelText("Task title"), {
    target: { value: "Read" },
  });
  fireEvent.change(screen.getByLabelText("Repeat every (days)"), {
    target: { value: "3" },
  });
  await screen.findByText(/Schedule preview: 2026-11-05/);
  fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  expect(create).toHaveBeenCalledWith("test", {
    title: "Read",
    description: "",
    priority: 2,
    anchor_date: "2026-11-05",
    interval_days: 3,
    reminder_enabled: false,
  });
});

it("requires a future re-anchor and retains the draft on a version conflict", async () => {
  preview();
  const update = vi
    .spyOn(recurrenceApi, "update")
    .mockRejectedValue(new ApiError("RECURRENCE_CONFLICT", 409));
  render(
    <MemoryRouter>
      <RecurrenceEditor
        rule={rule}
        day="2026-11-05"
        close={vi.fn()}
        saved={vi.fn()}
      />
    </MemoryRouter>,
  );
  fireEvent.change(screen.getByLabelText("Repeat every (days)"), {
    target: { value: "7" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("after today");
  expect(update).not.toHaveBeenCalled();
  fireEvent.change(
    screen.getByLabelText("Start date (changes apply to future tasks)"),
    {
      target: { value: "2026-11-06" },
    },
  );
  fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("Your draft is safe"),
  );
  expect(screen.getByLabelText("Repeat every (days)")).toHaveValue(7);
  expect(update).toHaveBeenCalledTimes(1);
  expect(update).toHaveBeenCalledWith(
    "test",
    "r",
    expect.objectContaining({ version: 1, anchor_date: "2026-11-06" }),
  );
});

it("pauses with a version and confirms deletion preserving history", async () => {
  vi.spyOn(recurrenceApi, "list").mockResolvedValue({
    items: [rule],
    next_cursor: "",
    plan_date: "2026-11-05",
    timezone: "Europe/Amsterdam",
    scheduler_enabled: false,
  });
  const update = vi
    .spyOn(recurrenceApi, "update")
    .mockResolvedValue({ ...rule, paused: true, version: 2 });
  const remove = vi.spyOn(recurrenceApi, "remove").mockResolvedValue(undefined);
  render(<RecurrenceBoard />);
  await screen.findByText("Read");
  expect(
    screen.getByText(/automatic task creation is not enabled yet/),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Pause" }));
  await waitFor(() =>
    expect(update).toHaveBeenCalledWith("test", "r", {
      version: 1,
      paused: true,
    }),
  );
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Delete" })).toBeEnabled(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  expect(
    screen.getByText(/Your existing tasks and history will stay/),
  ).toBeVisible();
  expect(remove).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Confirm delete" }));
  await waitFor(() => expect(remove).toHaveBeenCalledWith("test", rule));
});

it("shows the opt-in scheduler state from the API", async () => {
  vi.spyOn(recurrenceApi, "list").mockResolvedValue({
    items: [],
    next_cursor: "",
    plan_date: "2026-11-06",
    timezone: "Europe/Amsterdam",
    scheduler_enabled: true,
  });
  render(<RecurrenceBoard />);
  expect(await screen.findByText(/Automatic tasks are on/)).toBeVisible();
  expect(
    screen.queryByText(/automatic task creation is not enabled yet/),
  ).not.toBeInTheDocument();
});

it("requires a bound channel before enabling Discord reminders", async () => {
  vi.spyOn(discordApi, "status").mockResolvedValue({
    enabled: true,
    bound: false,
    version: 1,
    guild_id: null,
    channel_id: null,
    channel_name: null,
    bound_at: null,
  });
  preview();
  render(
    <MemoryRouter>
      <RecurrenceEditor day="2026-11-05" close={vi.fn()} saved={vi.fn()} />
    </MemoryRouter>,
  );
  await waitFor(() =>
    expect(screen.getByLabelText("Discord channel reminder")).toBeDisabled(),
  );
  expect(screen.getByRole("link", { name: "Profile" })).toHaveAttribute(
    "href",
    "/profile",
  );
});

it("saves the reminder choice only on a recurring rule", async () => {
  preview();
  const create = vi.spyOn(recurrenceApi, "create").mockResolvedValue(rule);
  render(
    <MemoryRouter>
      <RecurrenceEditor day="2026-11-05" close={vi.fn()} saved={vi.fn()} />
    </MemoryRouter>,
  );
  await waitFor(() =>
    expect(screen.getByLabelText("Discord channel reminder")).toBeEnabled(),
  );
  fireEvent.change(screen.getByLabelText("Task title"), {
    target: { value: "A gentle reminder" },
  });
  fireEvent.click(screen.getByLabelText("Discord channel reminder"));
  fireEvent.click(screen.getByRole("button", { name: "Save schedule" }));
  await waitFor(() =>
    expect(create).toHaveBeenCalledWith(
      "test",
      expect.objectContaining({ reminder_enabled: true }),
    ),
  );
});
