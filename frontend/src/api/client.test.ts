import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, errorMessage } from "./client";

afterEach(() => vi.unstubAllGlobals());
describe("API transport", () => {
  it("uses only same-origin requests, Bearer headers, and no persistence/cookies", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ id: "u" })));
    vi.stubGlobal("fetch", fetcher);
    await api.me("secret");
    const [url, options] = fetcher.mock.calls[0];
    expect(url).toBe("/api/auth/me");
    expect(options.headers.get("Authorization")).toBe("Bearer secret");
    expect(options.credentials).toBe("omit");
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
    await expect(api.logout("secret")).resolves.toBeUndefined();
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
    await expect(api.me("secret")).rejects.toMatchObject({
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
    await expect(api.me("secret")).rejects.toMatchObject({
      code: "HTTP_ERROR",
      status: 502,
    });
    await expect(api.me("secret")).rejects.toMatchObject({
      code: "NETWORK_ERROR",
    });
    await expect(api.me("secret")).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
    });
    const controller = new AbortController();
    controller.abort();
    fetcher.mockRejectedValueOnce(new DOMException("aborted", "AbortError"));
    await expect(api.me("secret", controller.signal)).rejects.toMatchObject({
      name: "AbortError",
    });
  });
});
