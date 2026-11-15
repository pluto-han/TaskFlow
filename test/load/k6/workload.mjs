// Pure workload definition shared by k6 and Node's offline contract tests.
export const profiles = { smoke: 30, baseline: 300, step: 120 };
const operations = [
  ...Array(8).fill('today'), ...Array(2).fill('filtered'),
  ...Array(2).fill('page'), ...Array(3).fill('history'),
  ...Array(3).fill('project'), 'create_today', 'update',
];

export function validateFixture(fixture) {
  if (!/^https:\/\/localhost:\d+$/.test(fixture.origin) ||
      !/^[a-f0-9]{20}$/.test(fixture.run_id) || !Object.hasOwn(profiles, fixture.profile) ||
      !['off', 'cold', 'warm'].includes(fixture.cache_mode) ||
      !['mixed', 'today-repeat'].includes(fixture.workload ?? 'mixed') ||
      ![1, 2, 4, 8, 16].includes(fixture.rps ?? 1) ||
      (fixture.profile !== 'step' && (fixture.rps ?? 1) !== 1) ||
      !Array.isArray(fixture.users) || fixture.users.length !== 10) {
    throw new Error('Only an orchestrator-created local ten-account fixture is supported');
  }
  for (const user of fixture.users) {
    if (!user.cookie || !user.csrf || !user.today_cursor || !user.project_id ||
        !Array.isArray(user.update_targets) || user.update_targets.length !== 20) {
      throw new Error('Incomplete preparation manifest');
    }
  }
}

export function requestFor(fixture, index) {
  if (fixture.workload === 'today-repeat') {
    return { user: fixture.users[0], operation: 'today', write: false, method: 'GET',
      path: '/api/tasks/today?limit=20', body: null, expected: 200 };
  }
  const cycle = Math.floor(index / operations.length);
  const user = fixture.users[(index + cycle) % fixture.users.length];
  const operation = operations[index % operations.length];
  const write = operation === 'create_today' || operation === 'update';
  let path = '/api/tasks/today?limit=20';
  let body = null;
  let method = 'GET';
  if (operation === 'filtered') path = '/api/tasks/today?limit=20&priority=2';
  if (operation === 'page') path = '/api/tasks/today?limit=10&cursor=' + encodeURIComponent(user.today_cursor);
  if (operation === 'history') path = '/api/tasks?limit=20&plan_date=' + fixture.history_date;
  if (operation === 'project') path = '/api/tasks?limit=20&project_id=' + user.project_id;
  if (operation === 'create_today') {
    method = 'POST';
    path = '/api/tasks/today';
    body = { title: `${user.title_prefix}load ${index}`, priority: 2, plan_date: fixture.plan_date };
  }
  if (operation === 'update') {
    // Step runs can exceed 400 requests. A given account/task is revisited only
    // every 400 iterations (>=25s at the capped 16 RPS, above the 5s timeout).
    // Derive versions deterministically without a shared cross-VU version cache.
    const round = fixture.profile === 'step' ? Math.floor(cycle / 20) : 0;
    const target = user.update_targets[fixture.profile === 'step' ? cycle % 20 : cycle];
    if (!target) throw new Error('Workload exceeded prepared update targets');
    method = 'PATCH';
    path = '/api/tasks/' + target.id;
    body = { version: target.version + round, completed: round % 2 === 0 };
  }
  return { user, operation, write, method, path, body, expected: method === 'POST' ? 201 : 200 };
}

export function validateResponse(fixture, request, data) {
  const { user, operation, body } = request;
  if (operation === 'create_today') {
    return data.title === body.title && data.plan_date === fixture.plan_date;
  }
  if (operation === 'update') {
    return data.id === request.path.split('/').pop() && data.completed === body.completed &&
      data.version === body.version + 1 && data.title.startsWith(user.title_prefix);
  }
  const items = operation === 'history' || operation === 'project' ? data : data.items;
  if (!Array.isArray(items) || items.length === 0 || items.length > 20) return false;
  if (operation === 'project') {
    return items.length === 20 && items.every(item => item.project_id === user.project_id &&
      item.title.startsWith(user.project_prefix));
  }
  if (!items.every(item => item.title.startsWith(user.title_prefix) && item.project_id === null)) return false;
  if (operation === 'history') {
    return items.length === 10 && items.every(item => item.plan_date === fixture.history_date);
  }
  return data.plan_date === fixture.plan_date && data.timezone === 'Europe/Amsterdam' &&
    items.every(item => item.plan_date === fixture.plan_date && (operation !== 'filtered' || item.priority === 2)) &&
    (operation !== 'today' || items.length === 20) &&
    (operation !== 'filtered' || items.length >= 8) &&
    (operation !== 'page' || items.length === 10);
}
