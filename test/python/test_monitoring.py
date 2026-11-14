"""Disposable Docker smoke test: real registry, Prometheus, Grafana and alert recovery.
No production project, user data, ports or credentials are used.
"""
import base64
import json
import os
from pathlib import Path
import secrets
import signal
import sys
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import urllib.parse

ROOT = Path(__file__).resolve().parents[2]


def run(args, **kwargs):
    result = subprocess.run(args, cwd=ROOT, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=kwargs.pop('timeout', 180), **kwargs)
    if result.returncode:
        raise RuntimeError(f'{args[0]} failed: {result.stderr[-2000:]}')
    return result.stdout.strip()


def eventually(fn, seconds=90):
    end = time.monotonic() + seconds
    last = None
    while time.monotonic() < end:
        try:
            value = fn()
            if value:
                return value
        except (OSError, ValueError, KeyError) as error:
            last = type(error).__name__
        time.sleep(1)
    raise RuntimeError(f'condition timed out ({last})')


def main():
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    name = 'taskflow-monitor-test-' + secrets.token_hex(6)
    artifacts = ROOT / 'artifacts/lesson26'
    artifacts.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='monitor-', dir=artifacts) as tmp:
        directory = Path(tmp)
        directory.chmod(0o755)
        secret = directory/'password'
        password = secrets.token_urlsafe(32)
        secret.write_text(password)
        secret.chmod(0o644)
        env = dict(os.environ, TASKFLOW_EDGE_NETWORK=name+'-edge')
        data = json.loads(run(['docker','compose','-f','compose.monitoring.yaml','config','--format','json'], env=env))
        data['name'] = name
        # Ensure the production definition does not publish exporters or allow anonymous access.
        assert not data['services']['prometheus'].get('ports')
        assert not data['services']['node-exporter'].get('ports')
        assert data['services']['grafana']['ports'][0]['host_ip'] == '127.0.0.1'
        assert data['services']['grafana']['environment']['GF_AUTH_ANONYMOUS_ENABLED'] == 'false'
        for volume in data['volumes'].values():
            volume.pop('name', None)
        for key, network in data['networks'].items():
            if key != 'taskflow_edge':
                network.pop('name', None)
        data['secrets']['grafana_password']['file'] = str(secret)
        # Docker does not publish ports on internal-only networks. Test-only ingress.
        data['services']['prometheus']['networks']['ingress'] = None
        for service, port in [('grafana',3000),('prometheus',9090)]:
            data['services'][service]['ports'] = [{'target':port,'published':'0','host_ip':'127.0.0.1','protocol':'tcp'}]
        # Accelerate hold periods only in this disposable test, not production rules.
        prom = directory/'prometheus.yml'
        prom.write_text((ROOT/'deploy/monitoring/prometheus.yml').read_text().replace('15s','1s').replace('scrape_timeout: 5s','scrape_timeout: 1s'))
        rules = directory/'alerts.yml'
        rules.write_text((ROOT/'deploy/monitoring/alerts.yml').read_text().replace('for: 2m','for: 2s'))
        for file in (prom,rules): file.chmod(0o644)
        for mount in data['services']['prometheus']['volumes']:
            if mount['target']=='/etc/prometheus/prometheus.yml':mount['source']=str(prom)
            if mount['target']=='/etc/prometheus/alerts.yml':mount['source']=str(rules)
        # Build a tiny fixture using the actual Go telemetry implementation.
        source = directory/'main.go'
        source.write_text('''package main
import("context";"net/http";"time";inframetrics "github.com/pluto-han/taskflow/internal/infra/metrics")
func main(){m:=inframetrics.New(nil,func(context.Context)error{return nil});m.Observe("GET","/tasks/:id",200,10*time.Millisecond)
mux:=http.NewServeMux();mux.Handle("/metrics",m.Handler())
mux.HandleFunc("/fixture/enable",func(w http.ResponseWriter,r *http.Request){m.SetCacheEnabled(true);w.Write([]byte("{}"))})
mux.HandleFunc("/fixture/hit",func(w http.ResponseWriter,r *http.Request){m.ObserveTodayCache("hit");m.ObserveTodayCacheFill("success");m.ObserveTodayCacheOperation("get",true,time.Millisecond);w.Write([]byte("{}"))})
http.ListenAndServe(":9091",mux)}
''')
        binary = directory/'fixture'
        run(['go','build','-o',str(binary),str(source)],env=dict(os.environ,CGO_ENABLED='0'),timeout=180)
        binary.chmod(0o755)
        # Use the existing minimal API image only as a shell-free executable carrier.
        data['services']['fixture']={'image':'taskflow-api:local','entrypoint':['/fixture'],
          'volumes':[{'type':'bind','source':str(binary),'target':'/fixture','read_only':True}],
          'networks':{'taskflow_edge':{'aliases':['api']},'ingress':None},'ports':[{'target':9091,'published':'0','host_ip':'127.0.0.1','protocol':'tcp'}],'read_only':True,'cap_drop':['ALL'],'mem_limit':'64m'}
        config = directory/'compose.json'
        config.write_text(json.dumps(data))
        def compose(*args): return run(['docker','compose','-p',name,'-f',str(config),*args])
        client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        def get(url, auth=False):
            headers = {}
            if auth: headers['Authorization']='Basic '+base64.b64encode(('taskflow-admin:'+password).encode()).decode()
            with client.open(urllib.request.Request(url,headers=headers),timeout=5) as response:
                return json.load(response)
        run(['docker','network','create','--internal',name+'-edge'])
        try:
            compose('up','-d','--no-build','--pull','never')
            grafana='http://'+compose('port','grafana','3000')
            prometheus='http://'+compose('port','prometheus','9090')
            fixture='http://'+compose('port','fixture','9091')
            eventually(lambda:get(grafana+'/api/health').get('database')=='ok')
            try:
                get(grafana+'/api/search')
                raise RuntimeError('Grafana allows unauthenticated dashboard access')
            except urllib.error.HTTPError as error:
                assert error.code==401
            dashboard=get(grafana+'/api/dashboards/uid/taskflow-overview',True)
            titles = {panel['title'] for panel in dashboard['dashboard']['panels']}
            assert {'API scrape availability', 'Database readiness',
                    'Background workers enabled', 'Seconds since successful background poll',
                    'Eligible notifications and oldest wait', 'Discord send attempts by outcome',
                    'Lease reclaims / second', 'Recurrence tasks generated / second',
                    'Today cache enabled', 'Today cache eligible hit ratio',
                    'Today cache reads / second', 'Today cache fill results / second',
                    'Today cache Redis operations / second', 'Today cache Redis operation p95'} <= titles
            def query(expr):
                return get(prometheus+'/api/v1/query?'+urllib.parse.urlencode({'query':expr}))['data']['result']
            eventually(lambda:query('taskflow_database_ready == 1'))
            assert query('taskflow_http_requests_total{route="/tasks/:id"}')
            ratio = next(panel['targets'][0]['expr'] for panel in dashboard['dashboard']['panels'] if panel['title']=='Today cache eligible hit ratio')
            eventually(lambda:query('taskflow_today_cache_enabled == 0'))
            assert not query(ratio), 'disabled cache must not show zero hit ratio'
            get(fixture+'/fixture/enable')
            eventually(lambda:query('taskflow_today_cache_enabled == 1'))
            assert not query(ratio), 'idle enabled cache must not show zero hit ratio'
            get(fixture+'/fixture/hit')
            eventually(lambda:query('taskflow_today_cache_reads_total{outcome="hit"} == 1'))
            eventually(lambda:query(ratio))
            # Execute every panel expression against real Prometheus; bad PromQL
            # or unknown functions must fail the test, even for an idle series.
            for panel in dashboard['dashboard']['panels']:
                for target in panel.get('targets', []): query(target['expr'])
            eventually(lambda:query('node_memory_MemAvailable_bytes{job="node"}'))
            eventually(lambda:query('node_filesystem_size_bytes{job="node",mountpoint="/"}'))
            # Real exporter outage: test firing and recovery, without touching real API.
            compose('stop','fixture')
            eventually(lambda:query('ALERTS{alertname="TaskFlowUnavailable",alertstate="firing"}'))
            compose('start','fixture')
            eventually(lambda:query('up{job="taskflow"} == 1'))
            eventually(lambda:not query('ALERTS{alertname="TaskFlowUnavailable"}'))
            print('PASS: real metric scrape, Grafana/cache panels and PromQL, disabled/idle hit ratio, alert firing and recovery')
            (artifacts/'monitoring-result.json').write_text(json.dumps({'status':'passed','checks':['scrape','grafana-auth','dashboard','cache-promql','disabled-idle-hit-ratio','alert-firing','alert-recovery']})+'\n')
        finally:
            compose('down','--volumes','--remove-orphans')
            run(['docker','network','rm',name+'-edge'])


if __name__=='__main__':
    main()
