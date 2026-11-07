import type {
  ErrorResponse,
  LoginRequest,
  LoginResponse,
  RegisterRequest,
  User,
} from "./types";

export class ApiError extends Error {
  constructor(
    public code: string,
    public status = 0,
    public requestId?: string,
  ) {
    super(code);
    this.name = "ApiError";
  }
}

// Fixed same-origin prefix: credentials cannot be redirected to a configured
// third-party origin. Dev proxy and production reverse proxy own the mapping.
export async function request<T>(
  path: string,
  method: string,
  body?: unknown,
  token?: string,
  signal?: AbortSignal,
  onResponse?: (response: Response) => void,
): Promise<T> {
  const headers = new Headers({ Accept: "application/json" });
  if (body !== undefined) headers.set("Content-Type", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  let response: Response;
  try {
    response = await fetch(`/api${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "omit",
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.any([
        AbortSignal.timeout(10_000),
        ...(signal ? [signal] : []),
      ]),
    });
  } catch (error) {
    if (signal?.aborted) throw error;
    throw new ApiError(
      error instanceof DOMException && error.name === "TimeoutError"
        ? "TIMEOUT"
        : "NETWORK_ERROR",
    );
  }
  if (!response.ok) {
    let code = "HTTP_ERROR";
    try {
      const payload = (await response.json()) as Partial<ErrorResponse>;
      if (typeof payload.error?.code === "string") code = payload.error.code;
    } catch {
      /* Proxy failures may be non-JSON; never display their raw body. */
    }
    throw new ApiError(
      code,
      response.status,
      response.headers.get("X-Request-ID") || undefined,
    );
  }
  onResponse?.(response);
  if (response.status === 204) return undefined as T;
  try {
    return (await response.json()) as T;
  } catch {
    throw new ApiError("INVALID_RESPONSE");
  }
}

export const api = {
  register: (input: RegisterRequest, signal?: AbortSignal) =>
    request<User>("/auth/register", "POST", input, undefined, signal),
  login: (input: LoginRequest, signal?: AbortSignal) =>
    request<LoginResponse>("/auth/login", "POST", input, undefined, signal),
  me: (token: string, signal?: AbortSignal) =>
    request<User>("/auth/me", "GET", undefined, token, signal),
  logout: (token: string, signal?: AbortSignal) =>
    request<void>("/auth/logout", "POST", undefined, token, signal),
};

export function errorMessage(error: unknown): string {
  if (!(error instanceof ApiError))
    return "We could not complete that just now. Please try again.";
  const messages: Record<string, string> = {
    RATE_LIMITED: "Too many requests. Please try again in a minute.",
    HTTPS_REQUIRED: "Please use the secure HTTPS address.",
    ORIGIN_NOT_ALLOWED:
      "This address is not allowed. Please use the official site.",
    FORBIDDEN:
      "You do not have permission to do this. Refresh the project to check your access.",
    INVITATION_INVALID:
      "This invitation has expired, has already been used, or belongs to another account. Please contact the project owner.",
    INVALID_INVITEE:
      "Check the account ID. Invite someone who has registered and is not already a member; ownership can only be transferred to a current member.",
    OWNER_REQUIRED:
      "Transfer ownership to a current member before leaving the project.",
    UNAUTHORIZED:
      "The email or password is incorrect, or your session has expired.",
    EMAIL_ALREADY_EXISTS: "An account already uses this email. Please sign in.",
    INVALID_USER: "Check that your email and password meet the requirements.",
    LOGIN_RATE_LIMITED: "Too many attempts. Please try again in a minute.",
    AUTH_BUSY: "Sign-in is busy right now. Please try again shortly.",
    REGISTRATION_BUSY:
      "Account creation is busy right now. Please try again shortly.",
    NETWORK_ERROR:
      "We could not reach the server. Check your connection and try again.",
    PROJECT_NOT_EMPTY:
      "This project still has tasks. Move or delete them before deleting the project.",
    INVALID_RECURRENCE:
      "Check the title, interval (1–365 days), and date. Schedule changes must start on a future date.",
    RECURRENCE_NOT_FOUND:
      "This schedule is no longer available. Please refresh the list.",
    RECURRENCE_CONFLICT:
      "This schedule has changed. Refresh it before editing again.",
    DISCORD_DISABLED: "Discord is not set up yet. Please try again later.",
    INVALID_DISCORD_BINDING:
      "This connection request is not valid. Refresh the status and try again.",
    DISCORD_CODE_RATE_LIMITED:
      "Too many connection codes requested. Please try again in a minute.",
    DISCORD_BINDING_CONFLICT:
      "The connection has changed. Close this dialog and refresh before deciding whether to disconnect.",
    TASK_NOT_FOUND:
      "This task is no longer available. Please refresh the list.",
    PROJECT_NOT_FOUND:
      "This project is no longer available, or you no longer have access. Please refresh your projects.",
    PLAN_DAY_CHANGED:
      "A new day has begun in Amsterdam. Your draft is safe; refresh the planning date before saving.",
    INVALID_QUERY:
      "These filters or this page are no longer valid. Please refresh the list.",
    TASK_CONFLICT:
      "This task has changed. Load the latest version before making further edits.",
    INVALID_TASK: "Please check the task title, priority, and due date.",
    INVALID_PROJECT:
      "Use 1–100 characters for the project name and up to 2,000 for the description.",
    TIMEOUT: "The request took too long. Please try again shortly.",
  };
  return (
    messages[error.code] ||
    "The service is taking a moment. Please try again shortly."
  );
}
