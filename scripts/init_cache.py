"""Prepare local Redis credentials; never start services or replace existing files."""
import hashlib
import os
from pathlib import Path
import secrets

ROOT = Path(__file__).resolve().parents[1]


def prepare(target):
    target = Path(target)
    if target.exists():
        raise SystemExit("Refusing to overwrite existing cache credentials")
    target.mkdir(parents=True, mode=0o700)
    target.chmod(0o700)
    password = secrets.token_hex(32)
    digest = hashlib.sha256(password.encode()).hexdigest()
    files = {
        "redis_password": password + "\n",
        "users.acl": (
            "user default reset off\n"
            f"user taskflow_cache reset on #{digest} ~taskflow:cache:* "
            "-@all +hello +ping +get +set +del\n"
        ),
    }
    for name, content in files.items():
        # Files are mounted read-only for UID 999. The host directory is 0700.
        fd = os.open(target / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as file:
            file.write(content)
        (target / name).chmod(0o644)


def main():
    prepare(ROOT / ".local/cache")
    print("Prepared .local/cache credentials; no services or API configuration changed")


if __name__ == "__main__":
    main()
