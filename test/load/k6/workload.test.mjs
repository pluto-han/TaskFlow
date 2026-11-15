import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateFixture, requestFor, validateResponse } from './workload.mjs';

function fixture() {
  return { run_id: 'a'.repeat(20), origin: 'https://localhost:12345', profile: 'baseline', cache_mode: 'cold',
    plan_date: '2026-11-15', history_date: '2026-11-14',
    users: Array.from({ length: 10 }, (_, i) => ({ cookie: `cookie-${i}`, csrf: 'csrf',
      title_prefix: `Account ${i} `, today_cursor: 'cursor+/=', project_id: `project-${Math.floor(i / 5)}`,
      project_prefix: `Project ${Math.floor(i / 5)} `,
      update_targets: Array.from({ length: 20 }, (_, j) => ({ id: `task-${i}-${j}`, version: 1 })),
    })) };
}

test('refuses arbitrary targets, incomplete accounts and unsupported profiles', () => {
  validateFixture(fixture());
  for (const patch of [{ origin: 'https://taskflow.hancongzhang.com' },
    { origin: 'http://localhost:12345' }, { profile: 'stress' }, { cache_mode: 'other' }, { users: [] }, { run_id: 'other' }]) {
    assert.throws(() => validateFixture({ ...fixture(), ...patch }));
  }
});

test('all cache modes keep identical requests and account assignment', () => {
  const f = fixture();
  for (const cache_mode of ['off', 'cold', 'warm']) {
    const other = { ...f, cache_mode };
    validateFixture(other);
    for (let i = 0; i < 301; i++) assert.deepEqual(requestFor(f, i), requestFor(other, i));
  }
});

test('TTL repeat reads one account only, with no writes or extra HTTP requests', () => {
  const f = { ...fixture(), workload: 'today-repeat' };
  validateFixture(f);
  const first = requestFor(f, 0);
  assert.equal(first.user.cookie, f.users[0].cookie);
  assert.equal(first.method, 'GET');
  assert.equal(first.body, null);
  assert.equal(first.write, false);
  assert.equal(first.path, '/api/tasks/today?limit=20');
  for (const cache_mode of ['off', 'cold', 'warm']) {
    for (let i = 0; i < 301; i++) assert.deepEqual(requestFor({ ...f, cache_mode }, i), first);
  }
  assert.throws(() => validateFixture({ ...f, workload: 'unbounded' }));
});

test('step rates are capped and reused update targets have consistent versions', () => {
  const f = { ...fixture(), profile: 'step', rps: 16 };
  validateFixture(f);
  assert.throws(() => validateFixture({ ...f, rps: 200 }));
  assert.throws(() => validateFixture({ ...f, profile: 'baseline' }));
  const versions = new Map();
  for (let i = 0; i < 1921; i++) {
    const r = requestFor(f, i);
    if (r.operation !== 'update') continue;
    const expected = versions.get(r.path) ?? 1;
    assert.equal(r.body.version, expected);
    assert.equal(r.body.completed, expected % 2 === 1);
    versions.set(r.path, expected + 1);
    assert.equal(validateResponse(f, r, { id: r.path.split('/').pop(),
      completed: r.body.completed, version: expected + 1, title: r.user.title_prefix + 'task' }), true);
  }
});

test('300 iterations have the exact workload mix and distinct optimistic writes', () => {
  const f = fixture();
  const counts = {}, users = new Set(), updates = new Set();
  for (let i = 0; i < 301; i++) {
    const r = requestFor(f, i);
    users.add(r.user.cookie);
    if (i < 300) counts[r.operation] = (counts[r.operation] || 0) + 1;
    if (r.operation === 'update') {
      assert.equal(r.body.version, 1);
      assert.ok(!updates.has(r.path));
      updates.add(r.path);
    }
    assert.ok(r.path.startsWith('/api/'));
    assert.ok(['GET', 'POST', 'PATCH'].includes(r.method));
  }
  assert.deepEqual(counts, { today: 120, filtered: 30, page: 30, history: 45,
    project: 45, create_today: 15, update: 15 });
  assert.equal(users.size, 10);
  assert.equal(updates.size, 15);
});

test('both smoke duration variants exercise each operation', () => {
  for (const count of [30, 31]) {
    const names = new Set(Array.from({ length: count }, (_, i) => requestFor(fixture(), i).operation));
    assert.equal(names.size, 7);
  }
});

test('personal scopes, historical dates, filters and page bounds are checked', () => {
  const f = fixture();
  for (const index of [0, 8, 10, 12]) {
    const r = requestFor(f, index);
    const item = { title: r.user.title_prefix + 'task', project_id: null, priority: 2,
      plan_date: r.operation === 'history' ? f.history_date : f.plan_date };
    const count = r.operation === 'today' ? 20 : r.operation === 'filtered' ? 8 : 10;
    const data = r.operation === 'history' ? Array(10).fill(item) :
      { items: Array(count).fill(item), plan_date: f.plan_date, timezone: 'Europe/Amsterdam' };
    assert.equal(validateResponse(f, r, data), true);
    const stolen = structuredClone(data);
    (Array.isArray(stolen) ? stolen : stolen.items)[0].title = 'Other account task';
    assert.equal(validateResponse(f, r, stolen), false);
  }
  const filtered = requestFor(f, 8);
  assert.equal(validateResponse(f, filtered, { items: [{ title: filtered.user.title_prefix + 'task',
    project_id: null, plan_date: f.plan_date, priority: 3 }],
    plan_date: f.plan_date, timezone: 'Europe/Amsterdam' }), false);
});

test('shared reads require the bound project and write responses require the new version', () => {
  const f = fixture(), r = requestFor(f, 15);
  const item = { project_id: r.user.project_id, title: r.user.project_prefix + 'task' };
  assert.equal(validateResponse(f, r, Array(20).fill(item)), true);
  assert.equal(validateResponse(f, r, Array(20).fill({ ...item, project_id: 'other' })), false);
  const update = requestFor(f, 19);
  const response = { id: update.path.split('/').pop(), title: update.user.title_prefix + 'task', completed: true, version: 2 };
  assert.equal(validateResponse(f, update, response), true);
  assert.equal(validateResponse(f, update, { ...response, version: 1 }), false);
});
