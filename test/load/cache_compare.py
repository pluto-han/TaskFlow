"""Local disposable Redis/PG HTTP comparison; accepts no production endpoint."""
import json
from pathlib import Path

from scripts.test_cache_stack import main

ROOT = Path(__file__).resolve().parents[2]


def write_summary():
    directory = ROOT / "artifacts/lesson42"
    data = json.loads((directory / "comparison.json").read_text())
    lines = [
        "# Today 缓存：本机隔离实验", "",
        "直连 Go API；目标 10 请求/秒；保持 Go 限流；没有 Nginx、TLS 或公网。",
        "每组逻辑数据相同，使用不同的测试账号。原始样本和环境见同目录 JSON。", "",
        "| 组 | 场景 | 成功读取 | 429 | 错误 | 列表 SQL | Session SQL | 成功/s | p95 ms | p99 ms | 最大调度滞后 ms |",
        "| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |",
    ]
    for row in data["rows"]:
        lines.append(
            f'| {row["group"]} | {row["scenario"]} | {row["successes"]} | '
            f'{row["http_429"]} | {row["errors"]} | {row["target_list_sql"]} | '
            f'{row["session_sql"]} | {row["success_per_second"]:.2f} | '
            f'{row["success_p95_ms"]:.3f} | {row["success_p99_ms"]:.3f} | '
            f'{row["max_start_lag_ms"]:.3f} |')
    lines += ["", "## 解释", ""]
    for scenario in ("same-user", "alternating-two-users"):
        off = next(row for row in data["rows"] if row["group"] == "disabled" and row["scenario"] == scenario)
        hot = next(row for row in data["rows"] if row["group"] == "hot" and row["scenario"] == scenario)
        reduction = 1 - (hot["target_list_sql"] / hot["successes"]) / (off["target_list_sql"] / off["successes"])
        lines.append(f'- {scenario}：热缓存每成功请求的目标列表 SQL 减少 {reduction:.1%}。仅适用于本次重复读取，不代表全库 SQL 或用户容量。')
    for row in data["rows"]:
        if row["group"] == "cold-to-warm":
            lines.append(f'- 冷转热 {row["scenario"]}：第一个请求 {row["samples"][0]["milliseconds"]:.3f} ms，列表 SQL {row["first_request_target_sql"]} 次；其余请求样本保留在 comparison.json。')
    lines += [
        '- Session SQL 没有因命中而省略；认证与缓存的职责保持分离。',
        '- mixed 行的 30 条成功数和延迟只包含读取；Session SQL 还包括 10 次创建，场景耗时包含写入和轮间等待。',
        '- overload-probe 没有固定速率，只确认现有限流会返回 429；其成功/s 不表示网站容量。',
        '- 并发过期可能执行多次列表 SQL，当前没有 singleflight。',
        '- 最大调度滞后、load average 和结束时资源快照可辅助检查本机负载；单次低速实验不能排除所有瓶颈。',
        '- 同机运行、样本少、组顺序固定、数据很小；结论需要按真实访问分布和多轮实验重新验证。', "",
    ]
    (directory / "summary.md").write_text("\n".join(lines))
    print("Measured report: artifacts/lesson42/summary.md (raw samples: comparison.json)")


if __name__ == "__main__":
    main(performance=True)
    write_summary()
