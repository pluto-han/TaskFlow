"""Bounded independent load steps; use only the disposable local stack runner."""
import argparse
from datetime import datetime, timezone
import json
import math
import signal
import subprocess
import sys
import time

from test.load.runner import ROOT, local_docker

RATES = (1, 2, 4, 8, 16)


def classify(report, exit_code):
    """A limit result requires successful non-429 checks and confirmed cleanup."""
    environment = json.loads((report / 'environment.json').read_text())
    if not environment.get('cleanup') or not (report / 'summary.json').exists() or not (report / 'operations.json').exists():
        return 'failed', {'cleanup': environment.get('cleanup', 'not confirmed')}
    metrics = json.loads((report / 'summary.json').read_text())['metrics']
    operations = json.loads((report / 'operations.json').read_text())
    statuses = operations['http_statuses']
    total = sum(statuses.values())
    record = {'cleanup': environment['cleanup'], 'iterations': metrics['iterations']['values']['count'],
              'business_success_rate': metrics['business_success']['values']['rate'],
              'http_statuses': statuses, 'operations': operations['operations'],
              'dropped_iterations': metrics.get('dropped_iterations', {}).get('values', {}).get('count'),
              'successful_read_p95_ms': metrics.get('read_duration', {}).get('values', {}).get('p(95)'),
              'limit_source_hint': {}}
    samples = report / 'samples.json'
    if samples.exists():
        for line in samples.open():
            try:
                point = json.loads(line)
            except json.JSONDecodeError:
                continue
            if point.get('type') == 'Point' and point.get('metric') == 'rate_limit_source':
                hint = point['data']['tags']['source']
                record['limit_source_hint'][hint] = record['limit_source_hint'].get(hint, 0) + point['data']['value']
    if exit_code == 0 and environment['status'] == 'passed':
        return 'passed', record
    if (exit_code == 1 and environment.get('threshold_failure') and total and
            total == record['iterations'] and not operations.get('incomplete_sample_lines') and
            record['dropped_iterations'] == 0 and
            set(statuses).issubset({'200', '201', '429'}) and statuses.get('429', 0) > 0 and
            (record['successful_read_p95_ms'] is None or record['successful_read_p95_ms'] < 500) and
            math.isclose(record['business_success_rate'], 1 - statuses['429'] / total, abs_tol=1e-12)):
        return 'limited', record
    return 'failed', record


def stop_child(process):
    if process.poll() is not None:
        return
    process.send_signal(signal.SIGTERM)
    # Give the child's finally block time to stop its owned Compose services.
    for _ in range(6):
        try:
            process.wait(timeout=15)
            return
        except subprocess.TimeoutExpired:
            pass
    process.kill()
    process.wait(timeout=15)
    raise RuntimeError('Child cleanup timed out; inspect the retained fixture before any further run')


def write_report(folder, records):
    (folder / 'steps.json').write_text(json.dumps(records, indent=2) + '\n')
    lines = ['# Isolated staircase', '',
             'Each rate uses a fresh fixture for 120 seconds; this is not a continuous ramp.',
             'Rate limits and resource guards remain enabled. Later steps stop on any non-pass.', '',
             '| Target RPS | Result | Iterations | HTTP statuses | Successful read p95 ms |',
             '| ---: | --- | ---: | --- | ---: |']
    for row in records:
        lines.append(f'| {row["rps"]} | {row["result"]} | {row.get("iterations", "unavailable")} | '
                     f'{json.dumps(row.get("http_statuses", {}))} | {row.get("successful_read_p95_ms", "unavailable")} |')
    lines += ['', 'A limited result is a configured acceptance boundary, not a measured CPU/database limit.',
              'Successful-request p95 excludes rejected requests; see status counts and raw samples.',
              'Gateway/API attribution uses Content-Type as a hint, not proof. Inspect server evidence if needed.',
              'Logs, step report paths, dropped iterations and cleanup status are preserved in steps.json.']
    (folder / 'summary.md').write_text('\n'.join(lines) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cache-mode', choices=('off', 'cold', 'warm'), default='cold')
    parser.add_argument('--workload', choices=('mixed', 'today-repeat'), default='mixed')
    args = parser.parse_args()
    local_docker()
    folder = ROOT / 'artifacts' / 'lesson72' / ('staircase-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ'))
    folder.mkdir(mode=0o700)
    records = []
    previous = signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    try:
        for rate in RATES:
            print(f'Starting {rate} RPS, fresh isolated fixture, 120-second measurement', flush=True)
            command = [sys.executable, '-u', '-m', 'test.load.runner', '--profile', 'step',
                       '--rps', str(rate), '--cache-mode', args.cache_mode, '--workload', args.workload]
            log_path = folder / f'{rate}-rps.log'
            row = {'rps': rate, 'result': 'failed', 'log': str(log_path.relative_to(ROOT))}
            records.append(row)
            write_report(folder, records)
            with log_path.open('w') as log:
                process = subprocess.Popen(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT,
                                           start_new_session=True)
                deadline = time.monotonic() + 1800
                progress = time.monotonic() + 30
                try:
                    while process.poll() is None:
                        if time.monotonic() >= deadline:
                            raise RuntimeError('Step exceeded total 30-minute preparation/execution budget')
                        if time.monotonic() >= progress:
                            print(f'{rate} RPS step running; progress log: {log_path.relative_to(ROOT)}', flush=True)
                            progress += 30
                        time.sleep(1)
                finally:
                    stop_child(process)
            report_lines = [line.removeprefix('Report: ').strip() for line in log_path.read_text().splitlines()
                            if line.startswith('Report: ')]
            row['exit_code'] = process.returncode
            if report_lines:
                report = (ROOT / report_lines[-1]).resolve()
                if report.parent != (ROOT / 'artifacts' / 'lesson72').resolve():
                    raise RuntimeError('Unexpected child report location')
                row['report'] = str(report.relative_to(ROOT))
                row['result'], details = classify(report, process.returncode)
                row.update(details)
            write_report(folder, records)
            print(f'{rate} RPS: {row["result"]}', flush=True)
            if row['result'] != 'passed':
                print('Stopped escalation; no higher rate was started.', flush=True)
                return 2 if row['result'] == 'limited' else 1
        return 0
    finally:
        signal.signal(signal.SIGTERM, previous)
        write_report(folder, records)
        print('Staircase report: ' + str(folder.relative_to(ROOT)), flush=True)


if __name__ == '__main__':
    sys.exit(main())
