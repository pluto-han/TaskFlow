import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, ApiError, errorMessage } from "../api/client";
import type { LoginRequest, User } from "../api/types";

type Session = { token: string; user: User; expiresAt: number };
type Auth = {
  user: User | null;
  expiresAt: number | null;
  busy: boolean;
  error: string;
  notice: string;
  login(input: LoginRequest, signal?: AbortSignal): Promise<void>;
  logout(): Promise<void>;
  refresh(): Promise<void>;
};
const AuthContext = createContext<Auth | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  // Credentials exist only in this mounted provider, never storage or URLs.
  const [session, setSession] = useState<Session | null>(null);
  const current = useRef<Session | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const operation = useRef<AbortController | null>(null);

  const save = useCallback((value: Session | null) => {
    current.current = value;
    setSession(value);
  }, []);
  const clear = useCallback(
    (message: string) => {
      operation.current?.abort();
      operation.current = null;
      save(null);
      setBusy(false);
      setError("");
      setNotice(message);
    },
    [save],
  );
  const begin = useCallback(() => {
    operation.current?.abort();
    const controller = new AbortController();
    operation.current = controller;
    setBusy(true);
    setError("");
    setNotice("");
    return controller;
  }, []);

  const login = useCallback(
    async (input: LoginRequest, signal?: AbortSignal) => {
      const controller = begin();
      const abort = () => controller.abort();
      signal?.addEventListener("abort", abort, { once: true });
      if (signal?.aborted) controller.abort();
      let token: string | undefined;
      try {
        const result = await api.login(input, controller.signal);
        token = result.access_token;
        controller.signal.throwIfAborted();
        const user = await api.me(token, controller.signal);
        controller.signal.throwIfAborted();
        const expiresAt = Date.parse(result.expires_at);
        if (!Number.isFinite(expiresAt) || expiresAt <= Date.now())
          throw new ApiError("UNAUTHORIZED", 401);
        if (!controller.signal.aborted) save({ token, user, expiresAt });
      } catch (err) {
        // A login can succeed while loading the profile fails. Revoke that unused
        // session best-effort; never retain its token in browser storage.
        if (token) void api.logout(token).catch(() => undefined);
        if (!controller.signal.aborted) throw err;
      } finally {
        signal?.removeEventListener("abort", abort);
        if (operation.current === controller) {
          operation.current = null;
          setBusy(false);
        }
      }
    },
    [begin, save],
  );

  const refresh = useCallback(async () => {
    const active = current.current;
    if (!active || operation.current) return;
    const controller = begin();
    try {
      const user = await api.me(active.token, controller.signal);
      if (!controller.signal.aborted && current.current === active)
        save({ ...active, user });
    } catch (err) {
      if (controller.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401)
        clear("登录已失效，请重新登录。");
      else setError(errorMessage(err));
    } finally {
      if (operation.current === controller) {
        operation.current = null;
        setBusy(false);
      }
    }
  }, [begin, clear, save]);

  const logout = useCallback(async () => {
    const active = current.current;
    if (!active) return;
    const controller = begin();
    try {
      await api.logout(active.token, controller.signal);
      if (!controller.signal.aborted) clear("已安全退出。");
    } catch (err) {
      if (controller.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401)
        clear("已退出，请重新登录。");
      else setError(`退出未完成。${errorMessage(err)}`);
    } finally {
      if (operation.current === controller) {
        operation.current = null;
        setBusy(false);
      }
    }
  }, [begin, clear]);

  useEffect(
    () => () => {
      operation.current?.abort();
    },
    [],
  );
  useEffect(() => {
    if (!session) return;
    const expire = () => {
      if (Date.now() >= session.expiresAt) clear("登录已过期，请重新登录。");
      else void refresh();
    };
    const timer = window.setTimeout(
      expire,
      Math.min(Math.max(0, session.expiresAt - Date.now()), 2_147_483_647),
    );
    window.addEventListener("focus", expire);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("focus", expire);
    };
  }, [session, clear, refresh]);

  return (
    <AuthContext.Provider
      value={{
        user: session?.user ?? null,
        expiresAt: session?.expiresAt ?? null,
        busy,
        error,
        notice,
        login,
        logout,
        refresh,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("AuthProvider is required");
  return value;
}
