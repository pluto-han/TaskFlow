"""Validate Prometheus rules with the exact pinned image, no network or server."""
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
config = json.loads(subprocess.check_output(
    ['docker','compose','-f','compose.monitoring.yaml','config','--format','json'],cwd=ROOT))
image = config['services']['prometheus']['image']
base = ['docker','run','--rm','--network','none','--entrypoint','/bin/promtool',
        '-v',str(ROOT/'deploy/monitoring')+':/checks:ro','-w','/checks',image]
for args in (['check','rules','alerts.yml'],['test','rules','alerts.test.yml']):
    subprocess.run(base+args,cwd=ROOT,check=True)
