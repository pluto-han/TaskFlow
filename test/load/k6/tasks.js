// Run only through scripts.test_load_stack: credentials and target are ephemeral.
import http from 'k6/http';
import exec from 'k6/execution';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { profiles, validateFixture, requestFor, validateResponse } from './workload.mjs';

const fixture = JSON.parse(open('/fixture/manifest.json'));
validateFixture(fixture);
const seconds = profiles[fixture.profile];
const rps = fixture.rps ?? 1;
const valid = new Rate('business_success');
const reads = new Trend('read_duration', true);
const readAttempts = new Trend('read_attempt_duration', true);
const outcomes = new Counter('business_status');
const unexpected = new Counter('unexpected_auth_conflict_limit');
const rateLimitSource = new Counter('rate_limit_source');
export const options = {
  scenarios: { [fixture.profile]: { executor: 'constant-arrival-rate', rate: rps, timeUnit: '1s',
    duration: `${seconds}s`, preAllocatedVUs: 10, maxVUs: 20, gracefulStop: '5s' } },
  maxRedirects: 0,
  systemTags: ['status', 'method', 'name', 'scenario', 'expected_response'],
  tags: { run_id: fixture.run_id, cache_mode: fixture.cache_mode, workload: fixture.workload ?? 'mixed', rps: String(rps) },
  thresholds: {
    business_success: [fixture.profile === 'smoke' ? 'rate==1' : 'rate>=0.99'],
    read_duration: ['p(95)<500'], dropped_iterations: ['count==0'],
    http_req_failed: [fixture.profile === 'smoke' ? 'rate==0' : 'rate<=0.01',
      { threshold: 'rate<=0.05', abortOnFail: true, delayAbortEval: '30s' }],
    unexpected_auth_conflict_limit: ['count==0'],
    // Arrival-rate scheduling can include the final boundary iteration.
    iterations: [`count>=${seconds * rps}`, `count<=${seconds * rps + 1}`],
  },
};

export default function () {
  const request = requestFor(fixture, exec.scenario.iterationInTest);
  const response = http.request(request.method, fixture.origin + request.path,
    request.body === null ? null : JSON.stringify(request.body),
    { timeout: '5s', redirects: 0, cookies: { '__Host-taskflow_session': request.user.cookie },
      headers: { Origin: fixture.origin, 'Content-Type': 'application/json',
        'X-CSRF-Token': request.user.csrf }, tags: { name: request.operation },
      responseCallback: http.expectedStatuses(request.expected) });
  let content = false;
  try {
    content = validateResponse(fixture, request, response.json());
  } catch (_) { /* Record failure without logging bodies or credentials. */ }
  const success = check(response, {
    'expected status and scoped content': r => r.status === request.expected && content,
  });
  const tags = { operation: request.operation };
  valid.add(success, tags);
  outcomes.add(1, { ...tags, status: String(response.status) });
  unexpected.add([401, 403, 409, 429].includes(response.status) ? 1 : 0);
  if (response.status === 429) {
    const type = String(response.headers['Content-Type'] ?? '').toLowerCase();
    rateLimitSource.add(1, { source: type.includes('text/html') ? 'gateway-html' :
      type.includes('application/json') ? 'api-json' : 'unknown' });
  }
  if (!request.write) {
    readAttempts.add(response.timings.duration, tags);
    if (success) reads.add(response.timings.duration, tags);
  }
}

export function handleSummary(data) {
  return { '/results/summary.json': JSON.stringify(data, null, 2) };
}
