import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useAuth } from "../auth/AuthProvider";
import { errorMessage } from "../api/client";
import { recurrenceApi, type Recurrence } from "../api/recurrences";
import { discordApi } from "../api/discord";
import { Link } from "react-router";
import { Modal } from "../components/Modal";

export function RecurrenceBoard() {
  const { authorize } = useAuth();
  const [items, setItems] = useState<Recurrence[]>([]);
  const [schedulerEnabled, setSchedulerEnabled] = useState(false);
  const [day, setDay] = useState("");
  const [cursor, setCursor] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState<Recurrence | "new" | null>(null);
  const [deleting, setDeleting] = useState<Recurrence | null>(null);
  const request = useRef<AbortController | null>(null);
  const load = useCallback(
    async (after = "") => {
      request.current?.abort();
      const controller = new AbortController();
      request.current = controller;
      setBusy(true);
      try {
        const page = await authorize((token) =>
          recurrenceApi.list(token, after, controller.signal),
        );
        if (controller.signal.aborted) return;
        setItems((old) => (after ? [...old, ...page.items] : page.items));
        setDay(page.plan_date);
        setSchedulerEnabled(page.scheduler_enabled);
        setCursor(page.next_cursor);
        setError("");
      } catch (err) {
        if (!controller.signal.aborted) setError(errorMessage(err));
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    },
    [authorize],
  );
  useEffect(() => {
    // Synchronize the collection with the server.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
    return () => request.current?.abort();
  }, [load]);
  async function mutate(action: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    try {
      await action();
      setDeleting(null);
      await load();
    } catch (err) {
      setError(errorMessage(err) + " 如发生版本冲突，请关闭弹窗并刷新后重试。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <section aria-label="循环任务">
      <div className="board-heading">
        <div>
          <span className="eyebrow">按自己的节奏</span>
          <h1>循环任务</h1>
          <p className="subtitle">按荷兰自然日重复，完成时间不改变周期。</p>
        </div>
        <button
          className="button primary"
          disabled={busy || !day}
          onClick={() => setEditor("new")}
        >
          新建循环规则
        </button>
      </div>
      <p className="hint">
        {schedulerEnabled
          ? "自动生成已启用：按荷兰日期加入今日任务，正常情况下约 30 秒内出现。"
          : "当前仅保存规则与预览日期，尚未启用自动生成任务。"}
      </p>
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      <button
        className="text-button"
        disabled={busy}
        onClick={() => void load()}
      >
        刷新规则
      </button>
      {busy && <p role="status">正在同步…</p>}
      {!busy && !error && !items.length && (
        <div className="empty-state">
          <span aria-hidden="true">○</span>
          <h2>还没有循环规则</h2>
          <p>从每天或每几天重复的小事开始。</p>
        </div>
      )}
      <ul className="task-list" aria-busy={busy}>
        {items.map((rule) => (
          <li className="task-row" key={rule.id}>
            <div className="task-content">
              <h2>{rule.title}</h2>
              {rule.description && (
                <p className="task-description">{rule.description}</p>
              )}
              <div className="task-meta">
                <span className={`priority priority-${rule.priority}`}>
                  {["", "低优先级", "中优先级", "高优先级"][rule.priority]}
                </span>
                <span>
                  每 {rule.interval_days} 天 · {rule.paused ? "已暂停" : "启用"}
                </span>
                {rule.reminder_enabled && <span>Discord 提醒</span>}
              </div>
              <p className="hint">
                {rule.paused
                  ? "恢复时跳过过去的日期。"
                  : `日期预览：${rule.preview_dates.join("、")}（荷兰）`}
              </p>
            </div>
            <div className="row-actions">
              <button
                className="text-button"
                disabled={busy}
                onClick={() => setEditor(rule)}
              >
                编辑
              </button>
              <button
                className="text-button"
                disabled={busy}
                onClick={() =>
                  void mutate(() =>
                    authorize((token) =>
                      recurrenceApi.update(token, rule.id, {
                        version: rule.version,
                        paused: !rule.paused,
                      }),
                    ),
                  )
                }
              >
                {rule.paused ? "恢复" : "暂停"}
              </button>
              <button
                className="text-button danger-text"
                disabled={busy}
                onClick={() => setDeleting(rule)}
              >
                删除
              </button>
            </div>
          </li>
        ))}
      </ul>
      {cursor && (
        <div className="load-more">
          <button
            className="button secondary"
            disabled={busy}
            onClick={() => void load(cursor)}
          >
            加载更多
          </button>
        </div>
      )}
      {editor && (
        <RecurrenceEditor
          rule={editor === "new" ? undefined : editor}
          day={day}
          close={() => setEditor(null)}
          saved={() => {
            setEditor(null);
            void load();
          }}
        />
      )}
      {deleting && (
        <Modal title="删除循环规则" busy={busy} close={() => setDeleting(null)}>
          {error && (
            <div className="notice error" role="alert">
              {error}
            </div>
          )}
          <p>删除“{deleting.title}”的规则？已有任务和历史记录会保留。</p>
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setDeleting(null)}
            >
              取消
            </button>
            <button
              className="button danger"
              disabled={busy}
              onClick={() =>
                void mutate(() =>
                  authorize((token) => recurrenceApi.remove(token, deleting)),
                )
              }
            >
              确认删除
            </button>
          </div>
        </Modal>
      )}
    </section>
  );
}

export function RecurrenceEditor({
  rule,
  day,
  close,
  saved,
}: {
  rule?: Recurrence;
  day: string;
  close(): void;
  saved(): void;
}) {
  const { authorize } = useAuth();
  const [title, setTitle] = useState(rule?.title ?? "");
  const [reminderEnabled, setReminderEnabled] = useState(
    rule?.reminder_enabled ?? false,
  );
  const [discordBound, setDiscordBound] = useState<boolean | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void authorize((token) => discordApi.status(token, controller.signal))
      .then((result) => {
        if (!controller.signal.aborted) setDiscordBound(result.bound);
      })
      .catch(() => {
        if (!controller.signal.aborted) setDiscordBound(false);
      });
    return () => controller.abort();
  }, [authorize]);
  const [description, setDescription] = useState(rule?.description ?? "");
  const [priority, setPriority] = useState(rule?.priority ?? 2);
  const [anchor, setAnchor] = useState(rule?.anchor_date ?? day);
  const [interval, setInterval] = useState(rule?.interval_days ?? 1);
  const [dates, setDates] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [previewError, setPreviewError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(() => {
      setDates([]);
      void authorize((token) =>
        recurrenceApi.preview(token, anchor, interval, controller.signal),
      )
        .then((result) => {
          if (!controller.signal.aborted) {
            setDates(result.dates);
            setPreviewError("");
          }
        })
        .catch((err) => {
          if (!controller.signal.aborted) setPreviewError(errorMessage(err));
        });
    }, 250);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [anchor, interval, authorize]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    if (
      rule &&
      (interval !== rule.interval_days || anchor !== rule.anchor_date) &&
      anchor <= day
    ) {
      setError("修改周期时，请选择晚于今天的生效日期。");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const body = {
        title: title.trim(),
        description,
        priority,
        interval_days: interval,
        reminder_enabled: reminderEnabled,
      };
      if (rule)
        await authorize((token) =>
          recurrenceApi.update(token, rule.id, {
            ...body,
            version: rule.version,
            ...(anchor !== rule.anchor_date || interval !== rule.interval_days
              ? { anchor_date: anchor }
              : {}),
          }),
        );
      else
        await authorize((token) =>
          recurrenceApi.create(token, { ...body, anchor_date: anchor }),
        );
      saved();
    } catch (err) {
      setError(
        errorMessage(err) +
          " 草稿已保留；版本冲突时请取消并刷新规则，再重新编辑。",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={rule ? "编辑循环规则" : "新建循环规则"}
      busy={busy}
      close={close}
    >
      {error && (
        <div className="notice error" role="alert">
          {error}
        </div>
      )}
      <form onSubmit={(event) => void submit(event)} aria-busy={busy}>
        <div className="field">
          <label htmlFor="rec-title">任务标题</label>
          <input
            id="rec-title"
            value={title}
            maxLength={200}
            required
            disabled={busy}
            onChange={(e) => setTitle(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="rec-description">描述</label>
          <textarea
            id="rec-description"
            rows={3}
            maxLength={2000}
            value={description}
            disabled={busy}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <div className="form-grid">
          <div className="field">
            <label htmlFor="rec-priority">优先级</label>
            <select
              id="rec-priority"
              value={priority}
              disabled={busy}
              onChange={(e) => setPriority(Number(e.target.value))}
            >
              <option value={1}>低</option>
              <option value={2}>中</option>
              <option value={3}>高</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="rec-interval">每隔几天</label>
            <input
              id="rec-interval"
              type="number"
              min={1}
              max={365}
              required
              value={interval}
              disabled={busy}
              onChange={(e) => {
                setInterval(Number(e.target.value));
                setDates([]);
              }}
            />
          </div>
        </div>
        <div className="field">
          <label htmlFor="rec-anchor">
            {rule ? "周期起点（改动后仅影响未来）" : "首次日期"}
          </label>
          <input
            id="rec-anchor"
            type="date"
            min={rule ? undefined : day}
            max="9999-12-31"
            value={anchor}
            required
            disabled={busy}
            onChange={(e) => {
              setAnchor(e.target.value);
              setDates([]);
            }}
          />
        </div>
        <div className="field reminder-option">
          <label htmlFor="rec-reminder">
            <input
              id="rec-reminder"
              type="checkbox"
              checked={reminderEnabled}
              disabled={busy || (!discordBound && !reminderEnabled)}
              onChange={(e) => setReminderEnabled(e.target.checked)}
            />{" "}
            Discord 频道提醒
          </label>
          {!discordBound && (
            <p className="hint">
              请先在<Link to="/profile">个人资料</Link>中绑定 Discord 频道。
            </p>
          )}
        </div>
        <p className="hint">
          使用 Europe/Amsterdam
          自然日。修改周期须选择未来的生效日期；不会修改已有任务，也不补过去任务。
        </p>
        <p className="hint" aria-live="polite">
          {previewError ||
            (dates.length ? `周期预览：${dates.join("、")}` : "正在计算日期…")}
        </p>
        <div className="modal-actions">
          <button
            type="button"
            className="button secondary"
            disabled={busy}
            onClick={close}
          >
            取消
          </button>
          <button type="submit" className="button primary" disabled={busy}>
            {busy ? "正在保存…" : "保存规则"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
