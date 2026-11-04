"""Real failure/recovery exercise, called inside the disposable local stack test."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile
from unittest.mock import patch
import deploy_release as cd

ROOT = Path(__file__).resolve().parents[1]


def drill(base, project, read_task):
    if not project.startswith('taskflow-smoke-'):
        raise RuntimeError('failure drill only accepts disposable smoke projects')
    result = subprocess.run(base+['config','--format','json'],cwd=ROOT,text=True,capture_output=True,check=True)
    previous = json.loads(result.stdout)
    candidate = copy.deepcopy(previous)
    # Real container startup failure, not a mocked Docker result.
    candidate['services']['api']['entrypoint'] = ['/intentional-missing-binary']
    candidate['services']['api']['restart'] = 'no'
    state_before = read_task()
    report = ROOT/'artifacts/lesson26'
    report.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='failure-drill-',dir=report) as tmp:
        old, bad = Path(tmp)/'previous.json', Path(tmp)/'candidate.json'
        old.write_text(json.dumps(previous));bad.write_text(json.dumps(candidate))
        # Same activation/rollback implementation; adapt only the isolated project
        # and its TLS test endpoint. Production never receives these overrides.
        with patch.object(cd,'ROOT',ROOT), patch.object(cd,'PROJECT',project), patch.object(cd,'probe',read_task):
            try:
                cd.activate_with_rollback(bad,old)
            except RuntimeError as error:
                if str(error) != 'deployment failed; previous images restored':raise
            else:
                raise RuntimeError('intentional broken release unexpectedly succeeded')
        assert read_task()==state_before, 'task changed across failed deployment'
    (report/'failure-drill-result.json').write_text(json.dumps({'status':'passed','checks':['real-container-startup-failure','same-rollback-code','task-and-session-preserved']})+'\n')
    print('PASS: failed release restored previous containers; task and Session preserved',flush=True)


if __name__=='__main__':
    import test_local_stack
    test_local_stack.main()
