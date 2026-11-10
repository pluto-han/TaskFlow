import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, errorMessage } from "../../../src/api/client";

afterEach(() => vi.unstubAllGlobals());
describe("API transport", () => {
  it("uses only same-origin cookies without exposing login credentials or using browser storage", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ id: "u" })));
    vi.stubGlobal("fetch", fetcher);
    await api.session();
    const [url, options] = fetcher.mock.calls[0];
    expect(url).toBe("/api/auth/browser/session");
    expect(options.headers.has("Authorization")).toBe(false);
    expect(options.credentials).toBe("same-origin");
    expect(options.redirect).toBe("error");
    expect(options.cache).toBe("no-store");
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });
  it("does not send credentials on login and accepts a bodyless logout", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(new Response("{}"))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetcher);
    await api.login({ email: "a@example.com", password: "long password" });
    expect(fetcher.mock.calls[0][1].headers.has("Authorization")).toBe(false);
    expect(fetcher.mock.calls[0][1].headers.has("X-CSRF-Token")).toBe(false);
    await expect(api.logout({ csrfToken: "secret" })).resolves.toBeUndefined();
    expect(fetcher.mock.calls[1][1].headers.get("X-CSRF-Token")).toBe("secret");
    expect(fetcher.mock.calls[1][1].headers.has("Authorization")).toBe(false);
  });
  it("preserves stable error codes without displaying server details", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: { code: "UNAUTHORIZED", message: "secret SQL" },
          }),
          { status: 401, headers: { "X-Request-ID": "request-1" } },
        ),
      ),
    );
    await expect(api.session()).rejects.toMatchObject({
      status: 401,
      code: "UNAUTHORIZED",
      requestId: "request-1",
    });
    expect(errorMessage(new ApiError("INTERNAL_ERROR"))).not.toContain("SQL");
  });
  it("handles proxy HTML, network errors, invalid JSON, and cancellation", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(new Response("<secret>", { status: 502 }))
      .mockRejectedValueOnce(new TypeError("secret-host"))
      .mockResolvedValueOnce(new Response("not JSON"));
    vi.stubGlobal("fetch", fetcher);
    await expect(api.session()).rejects.toMatchObject({
      code: "HTTP_ERROR",
      status: 502,
    });
    await expect(api.session()).rejects.toMatchObject({
      code: "NETWORK_ERROR",
    });
    await expect(api.session()).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
    const controller = new AbortController();
    controller.abort();
    fetcher.mockRejectedValueOnce(new DOMException("aborted", "AbortError"));
    await expect(api.session(controller.signal)).rejects.toMatchObject({
      name: "AbortError",
    });
  });
});

it("reads the pagination header without losing project filters", async () => {
  const { workspaceApi } = await import("../../../src/api/workspace");
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      new Response("[]", { headers: { "X-Next-Cursor": "next-page" } }),
    );
  vi.stubGlobal("fetch", fetcher);
  const page = await workspaceApi.tasks(
    { csrfToken: "secret" },
    { project: "none", completed: "false", priority: "3" },
    "a+b",
  );
  expect(page.next).toBe("next-page");
  const url = new URL(fetcher.mock.calls[0][0], "http://localhost");
  expect(url.searchParams.get("project_id")).toBe("none");
  expect(url.searchParams.get("cursor")).toBe("a+b");
  expect(url.searchParams.get("completed")).toBe("false");
});
