import { useState } from "react";
import { Link } from "react-router";
import { useAuth } from "../auth/AuthProvider";
import { sharingApi } from "../api/sharing";
import { errorMessage } from "../api/client";
export function InvitePage({
  token,
  consumed,
}: {
  token: string;
  consumed(): void;
}) {
  const { user, authorize } = useAuth();
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [joined, setJoined] = useState(false);
  async function accept() {
    setBusy(true);
    setError("");
    try {
      await authorize((session) => sharingApi.accept(session, token));
      setJoined(true);
      consumed();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="profile">
      <span className="eyebrow">WORK TOGETHER</span>
      <h1>项目邀请</h1>
      {joined ? (
        <>
          <p role="status">已加入项目。你可以查看、编辑和删除项目中的任务。</p>
          <Link className="button primary" to="/tasks">
            打开工作空间
          </Link>
        </>
      ) : !token ? (
        <p role="alert">
          邀请链接不可用。刷新会清除暂存的邀请，请重新打开原链接。
        </p>
      ) : !user ? (
        <>
          <p>请先登录接收邀请的账号。此邀请不能转交给其他账号使用。</p>
          <Link className="button primary" to="/login?next=invite">
            登录后接受邀请
          </Link>
        </>
      ) : (
        <>
          <p>当前账号：{user.email}</p>
          <p>
            接受后，你将以成员身份加入，能够查看、编辑和删除该项目中的任务。
          </p>
          {error && (
            <div className="notice error" role="alert">
              {error}
            </div>
          )}
          <button
            className="button primary"
            disabled={busy}
            onClick={() => void accept()}
          >
            {busy ? "正在加入…" : "接受邀请"}
          </button>
        </>
      )}
    </section>
  );
}
