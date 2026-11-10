import { expect, type Route } from "@playwright/test";

export function mockBrowserAuth(email: string) {
  let signedIn = false;
  const session = {
    user: { id: "u", email, created_at: "2026-01-01T00:00:00Z" },
    expires_at: "2099-01-01T00:00:00Z",
    csrf_token: "C".repeat(43),
  };
  return async (route: Route): Promise<boolean> => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/auth/browser/login") {
      signedIn = true;
      await route.fulfill({ json: session });
    } else if (path === "/api/auth/browser/logout") {
      expect(route.request().headers()["x-csrf-token"]).toBe(
        session.csrf_token,
      );
      signedIn = false;
      await route.fulfill({ status: 204 });
    } else if (path === "/api/auth/browser/session") {
      await route.fulfill(
        signedIn
          ? { json: session }
          : {
              status: 401,
              json: {
                error: { code: "UNAUTHORIZED", message: "Unauthorized" },
              },
            },
      );
    } else return false;
    expect(route.request().headers()["authorization"]).toBeUndefined();
    return true;
  };
}
