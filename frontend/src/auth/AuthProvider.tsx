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
import type {
  BrowserAuth,
  BrowserSession,
  LoginRequest,
  User,
} from "../api/types";

type Session = { credentials: BrowserAuth; user: User; expiresAt: number };
type Auth = {
  authorize<T>(operation: (credentials: BrowserAuth) => Promise<T>): Promise<T>;
  user: User | null;
  expiresAt: number | null;
  startup: "loading" | "ready" | "error";
  retry(): Promise<void>;
  busy: boolean;
  error: string;
  notice: string;
  login(input: LoginRequest, signal?: AbortSignal): Promise<void>;
  logout(): Promise<void>;
  refresh(): Promise<void>;
};
const AuthContext = createContext<Auth | null>(null);

function restored(value: BrowserSession): Session {
  const expiresAt = Date.parse(value.expires_at);
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now())
    throw new ApiError("UNAUTHORIZED", 401);
  if (!value.user?.id || !/^[A-Za-z0-9_-]{43}$/.test(value.csrf_token))
    throw new ApiError("INVALID_RESPONSE");
  return {
    user: value.user,
    expiresAt,
    credentials: { csrfToken: value.csrf_token },
  };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  // The login credential is in an HttpOnly cookie. Only CSRF/user data live here.
  const [session, setSession] = useState<Session | null>(null);
  const current = useRef<Session | null>(null);
  const [startup, setStartup] = useState<Auth["startup"]>("loading");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const operation = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const verification = useRef<Promise<boolean> | null>(null);
  const verified = useRef(true);
  const channel = useRef<BroadcastChannel | null>(null);
  const notify = useCallback(() => channel.current?.postMessage("changed"), []);
  const save = useCallback((value: Session | null) => {
    if (
      current.current?.user.id !== value?.user.id ||
      current.current?.credentials.csrfToken !== value?.credentials.csrfToken
    )
      generation.current++;
    verified.current = true;
    current.current = value;
    setSession(value);
  }, []);
  const clear = useCallback(
    (message: string) => {
      operation.current?.abort();
      operation.current = null;
      save(null);
      setStartup("ready");
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

  const restore = useCallback(
    async (gate: boolean) => {
      const hadSession = !!current.current;
      const controller = begin();
      verified.current = false;
      if (gate) {
        save(null);
        setStartup("loading");
      }
      try {
        const result = await api.session(controller.signal);
        controller.signal.throwIfAborted();
        save(restored(result));
        setStartup("ready");
        return true;
      } catch (err) {
        if (controller.signal.aborted) return false;
        if (err instanceof ApiError && err.status === 401) {
          clear(
            hadSession ? "Your session has ended. Please sign in again." : "",
          );
          return true;
        } else {
          setError(errorMessage(err));
          if (gate) setStartup("error");
          return false;
        }
      } finally {
        if (operation.current === controller) {
          operation.current = null;
          setBusy(false);
        }
      }
    },
    [begin, clear, save],
  );
  const retry = useCallback(async () => {
    await restore(true);
  }, [restore]);
  const refresh = useCallback(async () => {
    if (operation.current || !current.current) return;
    const pending = restore(false);
    verification.current = pending;
    await pending;
    if (verification.current === pending) verification.current = null;
  }, [restore]);

  const login = useCallback(
    async (input: LoginRequest, signal?: AbortSignal) => {
      const controller = begin();
      const abort = () => controller.abort();
      signal?.addEventListener("abort", abort, { once: true });
      if (signal?.aborted) controller.abort();
      try {
        const result = await api.login(input, controller.signal);
        controller.signal.throwIfAborted();
        save(restored(result));
        setStartup("ready");
        notify();
      } catch (err) {
        // Do not revoke via a cancelled response: the shared cookie may already
        // belong to a newer login in another tab. Reconcile on next bootstrap/focus.
        if (!controller.signal.aborted) throw err;
      } finally {
        signal?.removeEventListener("abort", abort);
        if (operation.current === controller) {
          operation.current = null;
          setBusy(false);
        }
      }
    },
    [begin, notify, save],
  );

  const logout = useCallback(async () => {
    const active = current.current;
    if (!active) return;
    const controller = begin();
    try {
      await api.logout(active.credentials, controller.signal);
      controller.signal.throwIfAborted();
      clear("You have signed out safely.");
      notify();
    } catch (err) {
      if (controller.signal.aborted) return;
      if (err instanceof ApiError && err.status === 401) {
        clear("You are signed out. Please sign in again.");
        notify();
      } else if (err instanceof ApiError && err.code === "CSRF_INVALID") {
        await restore(true);
      } else setError(`We could not sign you out. ${errorMessage(err)}`);
    } finally {
      if (operation.current === controller) {
        operation.current = null;
        setBusy(false);
      }
    }
  }, [begin, clear, notify, restore]);

  useEffect(() => {
    // Bootstrap synchronizes React with the server session; loading starts true.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void restore(true);
    if (typeof window.BroadcastChannel === "function") {
      channel.current = new BroadcastChannel("taskflow:session");
      channel.current.onmessage = (event: MessageEvent<unknown>) => {
        if (event.data === "changed") void restore(true);
      };
    }
    const focus = () => {
      if (!operation.current && current.current) void refresh();
      else if (!operation.current) void restore(false);
    };
    window.addEventListener("focus", focus);
    return () => {
      operation.current?.abort();
      channel.current?.close();
      channel.current = null;
      window.removeEventListener("focus", focus);
    };
  }, [restore, refresh]);

  useEffect(() => {
    if (!session) return;
    const timer = window.setTimeout(
      () => {
        clear("Your session has expired. Please sign in again.");
        notify();
      },
      Math.min(Math.max(0, session.expiresAt - Date.now()), 2_147_483_647),
    );
    return () => window.clearTimeout(timer);
  }, [session, clear, notify]);

  const authorize = useCallback(
    async <T,>(
      action: (credentials: BrowserAuth) => Promise<T>,
    ): Promise<T> => {
      const active = current.current;
      const epoch = generation.current;
      if (!active) throw new ApiError("UNAUTHORIZED", 401);
      try {
        // A later explicit user action may recover a failed background check.
        if (!verified.current && !verification.current) await refresh();
        if (verification.current) await verification.current;
        if (generation.current !== epoch)
          throw new DOMException("Session changed", "AbortError");
        if (!verified.current) throw new ApiError("NETWORK_ERROR");
        const result = await action(active.credentials);
        if (verification.current) await verification.current;
        if (!verified.current) throw new ApiError("NETWORK_ERROR");
        if (generation.current !== epoch)
          throw new DOMException("Session changed", "AbortError");
        return result;
      } catch (err) {
        if (verification.current) await verification.current;
        if (generation.current !== epoch)
          throw new DOMException("Session changed", "AbortError");
        if (!verified.current) throw new ApiError("NETWORK_ERROR");
        if (err instanceof ApiError && err.status === 401) {
          clear("Your session has ended. Please sign in again.");
          notify();
        } else if (
          err instanceof ApiError &&
          (err.code === "CSRF_INVALID" || err.code === "AMBIGUOUS_CREDENTIALS")
        ) {
          // Recheck identity, but never retry a business mutation automatically.
          void restore(true);
        }
        throw err;
      }
    },
    [clear, notify, restore, refresh],
  );

  return (
    <AuthContext.Provider
      value={{
        authorize,
        user: session?.user ?? null,
        expiresAt: session?.expiresAt ?? null,
        startup,
        retry,
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
