"""Generate local-only credentials and a short-lived test CA; never overwrite."""
from pathlib import Path
import os
import secrets
import subprocess

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / '.local' / 'stack'

def openssl(*args):
    subprocess.run(['openssl', *args], cwd=TARGET, check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

def main():
    if TARGET.exists():
        raise SystemExit('Refusing to overwrite .local/stack. Existing volumes need their original credentials.')
    TARGET.mkdir(parents=True, mode=0o700)
    os.chmod(TARGET.parent, 0o700)
    os.umask(0o077)
    admin, app = secrets.token_hex(32), secrets.token_hex(32)
    for name, value in {'admin_password': admin, 'app_password': app,
            'admin_url': f'postgres://taskflow_migrator:{admin}@postgres:5432/taskflow?sslmode=verify-full&sslrootcert=/run/secrets/db_ca',
            'app_url': f'postgres://taskflow_app:{app}@postgres:5432/taskflow?sslmode=verify-full&sslrootcert=/run/secrets/db_ca'}.items():
        (TARGET / name).write_text(value + '\n')
    openssl('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '30',
            '-keyout', 'ca.key', '-out', 'ca.crt', '-subj', '/CN=TaskFlow local test CA',
            '-addext', 'basicConstraints=critical,CA:TRUE',
            '-addext', 'keyUsage=critical,keyCertSign,cRLSign')
    for name, cn, san in [('db', 'postgres', 'DNS:postgres'), ('web', 'localhost', 'DNS:localhost,IP:127.0.0.1')]:
        openssl('req', '-newkey', 'rsa:2048', '-nodes', '-keyout', name+'.key',
                '-out', name+'.csr', '-subj', '/CN='+cn)
        (TARGET / (name+'.ext')).write_text('subjectAltName='+san+'\nextendedKeyUsage=serverAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\n')
        openssl('x509', '-req', '-in', name+'.csr', '-CA', 'ca.crt', '-CAkey', 'ca.key',
                '-CAcreateserial', '-out', name+'.crt', '-days', '30', '-extfile', name+'.ext')
    # Compose file secrets preserve host permissions. Parent directories are 0700;
    # files must be readable by numeric non-root container users via bind mounts.
    for p in TARGET.iterdir():
        p.chmod(0o600 if p.name=='ca.key' else 0o644)
    print('Created .local/stack (ignored by Git/build context). Test CA expires in 30 days; not for public deployment.')

if __name__ == '__main__':
    main()
