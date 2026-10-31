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
  if (!(error instanceof ApiError)) return "暂时无法完成操作，请重试。";
  const messages: Record<string, string> = {
    RATE_LIMITED: "请求过于频繁，请一分钟后重试。",
    HTTPS_REQUIRED: "请通过 HTTPS 安全入口访问服务。",
    ORIGIN_NOT_ALLOWED: "当前访问来源未获允许，请使用正式站点。",
    FORBIDDEN: "你没有执行此操作的权限，请刷新项目权限。",
    INVITATION_INVALID:
      "邀请已失效、已使用，或当前账号不是接收者。请联系项目所有者。",
    INVALID_INVITEE:
      "请检查账号 ID；接收者需已注册且尚未加入，转移对象必须是现有成员。",
    OWNER_REQUIRED: "请先将所有权转移给现有成员，再退出项目。",
    UNAUTHORIZED: "邮箱或密码不正确，或登录状态已失效。",
    EMAIL_ALREADY_EXISTS: "这个邮箱已注册，请直接登录。",
    INVALID_USER: "请检查邮箱和密码是否符合要求。",
    LOGIN_RATE_LIMITED: "尝试次数较多，请一分钟后再试。",
    AUTH_BUSY: "登录服务繁忙，请稍后重试。",
    REGISTRATION_BUSY: "注册服务繁忙，请稍后重试。",
    NETWORK_ERROR: "暂时无法连接服务器，请检查网络后重试。",
    PROJECT_NOT_EMPTY: "项目仍有任务，请先移走或删除任务。",
    TASK_NOT_FOUND: "任务已不存在，请刷新列表。",
    PROJECT_NOT_FOUND: "项目已不存在或无法访问，请刷新项目列表。",
    TASK_CONFLICT: "任务已被修改，请读取最新版本后再决定如何编辑。",
    INVALID_TASK: "请检查任务标题、优先级与截止日期。",
    INVALID_PROJECT: "项目名称需为 1–100 个字符，描述最多 2000 个字符。",
    TIMEOUT: "请求超时，请稍后重试。",
  };
  return messages[error.code] || "服务暂时不可用，请稍后重试。";
}
