"""Render VM Compose configurations without starting services or using local secrets."""

import json
import os
from pathlib import Path
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parent.parent
PROVIDERS = {
    "AUTH_SOCIAL_GOOGLE_CLIENT_ID": "google-fixture",
    "AUTH_SOCIAL_GOOGLE_CLIENT_SECRET_REF": "file:/etc/zed-auth/secrets/google-fixture",
    "AUTH_SOCIAL_MICROSOFT_CLIENT_ID": "microsoft-fixture",
    "AUTH_SOCIAL_MICROSOFT_CLIENT_SECRET_REF": "file:/etc/zed-auth/secrets/microsoft-fixture",
    "AUTH_SOCIAL_MICROSOFT_TENANT_ID": "11111111-1111-4111-8111-111111111111",
    "AUTH_SOCIAL_GITHUB_CLIENT_ID": "github-fixture",
    "AUTH_SOCIAL_GITHUB_CLIENT_SECRET_REF": "file:/etc/zed-auth/secrets/github-fixture",
}


def main():
    # Ignore workstation AUTH_* values and .env files, including real credentials.
    env = {key: value for key, value in os.environ.items() if not key.startswith("AUTH_")}
    env.update({
        "AUTH_ENV": "staging",
        "AUTH_DOMAIN": "ci.example.com",
        "AUTH_ISSUER": "https://ci.example.com",
        "AUTH_IMAGE": "example/auth@sha256:" + "0" * 64,
        "AUTH_POSTGRES_DSN": "postgres://auth_app:local_dev_only@postgres:5432/auth",
        "AUTH_POSTGRES_APP_PASSWORD": "local_dev_only",
        "AUTH_POSTGRES_OWNER_PASSWORD": "local_dev_only",
        "AUTH_MIGRATE_DSN": "postgres://auth_owner:local_dev_only@postgres/auth",
        "AUTH_REDIS_PASSWORD": "local_dev_only",
        "AUTH_SECRETS_DIR": "/tmp/fixture-secrets",
        "AUTH_ADMIN_TOKEN_REF": "file:/etc/zed-auth/secrets/fixture-admin",
    })
    with tempfile.TemporaryDirectory() as directory:
        empty_env = Path(directory) / "empty.env"
        empty_env.write_text("", encoding="utf-8")
        for filename in ("docker-compose.yml", "docker-compose.tunnel.yml"):
            for enabled in (False, True):
                fixture_env = env | (PROVIDERS if enabled else {})
                result = subprocess.run(
                    ["docker", "compose", "--env-file", str(empty_env), "-f",
                     str(ROOT / "deploy" / "vm" / filename), "config", "--format", "json"],
                    env=fixture_env, capture_output=True, text=True, check=False,
                )
                if result.returncode:
                    raise SystemExit(f"{filename}: Compose rendering failed (exit {result.returncode})")
                service = json.loads(result.stdout)["services"]["authservice"]
                actual = service["environment"]
                for key, value in PROVIDERS.items():
                    if actual.get(key) != (value if enabled else ""):
                        raise SystemExit(f"{filename}: {key} does not preserve optional configuration")
                if any(key in actual for key in ("AUTH_POSTGRES_OWNER_PASSWORD", "AUTH_MIGRATE_DSN")):
                    raise SystemExit(f"{filename}: owner credentials reach the service")
                if "owner-must-not-reach-service" in json.dumps(service):
                    raise SystemExit(f"{filename}: owner credential value reaches the service")
                if enabled:
                    mounts = service.get("volumes", [])
                    if not any(mount.get("target") == "/etc/zed-auth/secrets" and
                               mount.get("read_only") is True for mount in mounts):
                        raise SystemExit(f"{filename}: provider secret mount is not read-only")
                print(f"{filename}: social {'configured' if enabled else 'disabled'}, owner credentials isolated")
    print("4 VM Compose configuration checks passed")


if __name__ == "__main__":
    main()
