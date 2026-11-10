import { useEffect } from "react";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { App } from "../../../src/App";
import { api, ApiError } from "../../../src/api/client";
import { AuthProvider, useAuth } from "../../../src/auth/AuthProvider";
import { discordApi } from "../../../src/api/discord";

const session = (id = "a") => ({
  user: { id, email: `${id}@example.com`, created_at: "2026-01-01T00:00:00Z" },
  expires_at: new Date(Date.now() + 3_600_000).toISOString(),
  csrf_token: id.toUpperCase().repeat(43),
});
beforeEach(() => {
  vi.spyOn(api, "session").mockRejectedValue(new ApiError("UNAUTHORIZED", 401));
  vi.spyOn(api, "login").mockResolvedValue(session());
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
afterEach(() => vi.unstubAllGlobals());
const app = () =>
  render(
    <MemoryRouter initialEntries={["/profile"]}>
      <App />
    </MemoryRouter>,
  );

it("waits for bootstrap without flashing the login page", async () => {
  let finish!: (value: ReturnType<typeof session>) => void;
  vi.mocked(api.session).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  app();
  expect(screen.getByRole("status")).toHaveTextContent("check your sign-in");
  expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  await act(async () => finish(session()));
  expect(
    await screen.findByRole("heading", { name: "A space of your own" }),
  ).toBeVisible();
});

it.each([new ApiError("NETWORK_ERROR"), new ApiError("INTERNAL_ERROR", 500)])(
  "keeps startup errors retryable instead of treating them as logout: %s",
  async (err) => {
    vi.mocked(api.session).mockRejectedValueOnce(err);
    app();
    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
    vi.mocked(api.session).mockResolvedValue(session());
    await act(async () =>
      screen.getByRole("button", { name: "Try again" }).click(),
    );
    expect(
      await screen.findByRole("heading", { name: "A space of your own" }),
    ).toBeVisible();
  },
);

it("treats an expired bootstrap session as anonymous", async () => {
  vi.mocked(api.session).mockResolvedValue({
    ...session(),
    expires_at: "2000-01-01T00:00:00Z",
  });
  app();
  expect(
    await screen.findByRole("heading", { name: "Welcome back." }),
  ).toBeVisible();
});

class Channel {
  static instances: Channel[] = [];
  messages: unknown[] = [];
  onmessage: ((event: MessageEvent) => void) | null = null;
  closed = false;
  constructor() {
    Channel.instances.push(this);
  }
  postMessage(data: unknown) {
    this.messages.push(data);
  }
  close() {
    this.closed = true;
  }
  receive(data: unknown) {
    this.onmessage?.({ data } as MessageEvent);
  }
}
let auth!: ReturnType<typeof useAuth>;
function Probe() {
  const value = useAuth();
  useEffect(() => {
    auth = value;
  }, [value]);
  return <p>{value.user?.id ?? value.startup}</p>;
}
function provider() {
  return render(
    <AuthProvider>
      <Probe />
    </AuthProvider>,
  );
}

it("revalidates tab notifications, never broadcasts credentials or echoes messages", async () => {
  Channel.instances = [];
  vi.stubGlobal("BroadcastChannel", Channel);
  const view = provider();
  await waitFor(() => expect(auth.startup).toBe("ready"));
  await act(async () =>
    auth.login({ email: "a@example.com", password: "password" }),
  );
  const channel = Channel.instances[0];
  expect(channel.messages).toEqual(["changed"]);
  vi.mocked(api.session).mockResolvedValue(session("b"));
  await act(async () => channel.receive("changed"));
  expect(auth.user?.id).toBe("b");
  expect(channel.messages).toEqual(["changed"]);
  const calls = vi.mocked(api.session).mock.calls.length;
  await act(async () => channel.receive({ token: "untrusted" }));
  expect(api.session).toHaveBeenCalledTimes(calls);
  view.unmount();
  expect(channel.closed).toBe(true);
});

it("rejects a late business response after another account becomes active", async () => {
  Channel.instances = [];
  vi.stubGlobal("BroadcastChannel", Channel);
  vi.mocked(api.session).mockResolvedValue(session());
  provider();
  await waitFor(() => expect(auth.user?.id).toBe("a"));
  let finish!: (value: string) => void;
  const pending = auth.authorize(
    () =>
      new Promise<string>((resolve) => {
        finish = resolve;
      }),
  );
  // Install the rejection handler before releasing the response.
  const rejected = expect(pending).rejects.toMatchObject({
    name: "AbortError",
  });
  vi.mocked(api.session).mockResolvedValue(session("b"));
  await act(async () => Channel.instances[0].receive("changed"));
  finish("private account A result");
  await rejected;
  expect(auth.user?.id).toBe("b");
});

it("does not automatically replay a write after CSRF failure", async () => {
  vi.mocked(api.session).mockResolvedValue(session());
  provider();
  await waitFor(() => expect(auth.user?.id).toBe("a"));
  vi.mocked(api.session).mockResolvedValue(session("b"));
  const write = vi.fn().mockRejectedValue(new ApiError("CSRF_INVALID", 403));
  await act(async () => {
    await expect(auth.authorize(write)).rejects.toMatchObject({
      code: "CSRF_INVALID",
    });
  });
  await waitFor(() => expect(auth.user?.id).toBe("b"));
  expect(write).toHaveBeenCalledOnce();
});

it("keeps an anonymous form draft when focus rechecks the session", async () => {
  app();
  const email = await screen.findByLabelText("Email address");
  await act(async () => {
    fireEvent.change(email, { target: { value: "draft@example.com" } });
    window.dispatchEvent(new Event("focus"));
  });
  expect(screen.getByLabelText("Email address")).toBe(email);
  expect(email).toHaveValue("draft@example.com");
});

it("waits for focus verification before applying an old request's 401", async () => {
  vi.mocked(api.session).mockResolvedValue(session());
  provider();
  await waitFor(() => expect(auth.user?.id).toBe("a"));
  let fail!: (error: Error) => void;
  const pending = auth.authorize(
    () =>
      new Promise<string>((_, reject) => {
        fail = reject;
      }),
  );
  const rejected = expect(pending).rejects.toMatchObject({
    name: "AbortError",
  });
  let finish!: (value: ReturnType<typeof session>) => void;
  vi.mocked(api.session).mockReturnValue(
    new Promise((resolve) => {
      finish = resolve;
    }),
  );
  await act(async () => window.dispatchEvent(new Event("focus")));
  fail(new ApiError("UNAUTHORIZED", 401));
  await act(async () => finish(session("b")));
  await rejected;
  expect(auth.user?.id).toBe("b");
});

it("rechecks a failed background verification before a new explicit action", async () => {
  vi.mocked(api.session).mockResolvedValue(session());
  provider();
  await waitFor(() => expect(auth.user?.id).toBe("a"));
  vi.mocked(api.session).mockRejectedValueOnce(new ApiError("NETWORK_ERROR"));
  await act(async () => window.dispatchEvent(new Event("focus")));
  const write = vi.fn().mockResolvedValue("saved");
  await act(async () => {
    await expect(auth.authorize(write)).resolves.toBe("saved");
  });
  expect(api.session).toHaveBeenCalledTimes(3);
  expect(write).toHaveBeenCalledOnce();
});
