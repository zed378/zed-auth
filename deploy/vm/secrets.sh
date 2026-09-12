#!/usr/bin/env bash
# Creates and repairs the secrets directory the service reads from.
#
# This script exists because "chmod 400" is necessary and not sufficient, and
# the gap between those two costs a restart loop to discover.
#
# The service runs in a distroless :nonroot image as uid 65532. The secrets
# directory is a bind mount from the host, so the host's ownership is what the
# container sees. A directory that is mode 700 and owned by the operator is
# unreadable to the service no matter what mode the files inside it have — the
# container cannot even traverse it, and the failure surfaces as
# "stat ...: permission denied" on a file that plainly exists.
#
# Meanwhile the resolver refuses any secret file with group or world bits set
# (P0-14), so the obvious repair — making the file group-readable — is
# correctly rejected.
#
# That leaves exactly one shape: the directory and its contents belong to the
# service's uid, mode 0700 and 0400. The operator is not the service and reads
# these files with sudo, which is the right relationship anyway
# (deploy/SECRETS.md).
set -euo pipefail

# distroless/static-debian12:nonroot. Pinned here rather than derived, because
# deriving it would mean trusting a running container to tell us how to secure
# the files it reads.
readonly SERVICE_UID=65532
readonly SERVICE_GID=65532

SECRETS_DIR="${AUTH_SECRETS_DIR:-/etc/zed-auth/secrets}"

# Whether the caller chose the path, or it defaulted.
#
# `sudo` strips the environment. So the runbook's `sudo secrets.sh fix`, on a
# host whose secrets live anywhere but the default, silently operates on the
# default — creating a directory nothing reads and, for `metrics-token`,
# writing a real credential into it.
#
# That is not hypothetical: it happened on 2026-09-11 while rebuilding this
# VM's secrets directory. The command reported success, the service kept
# failing for the original reason, and a live token was left at a path no
# inventory knows about.
SECRETS_DIR_WAS_CHOSEN=${AUTH_SECRETS_DIR:+yes}

usage() {
  cat >&2 <<USAGE
usage: $(basename "$0") <command>

  fix                Create the secrets directory if absent and correct the
                     ownership and mode of everything in it. Idempotent.
  metrics-token      Generate the metrics scrape token if it does not exist.
  show <name>        Print one secret to stdout.

Environment:
  AUTH_SECRETS_DIR   Defaults to /etc/zed-auth/secrets.
USAGE
  exit 64
}

require_root() {
  if [[ $EUID -ne 0 ]]; then
    echo "$(basename "$0"): must run as root (the files are owned by uid ${SERVICE_UID})" >&2
    exit 1
  fi
}

# refuse_to_guess stops before creating a secrets directory somewhere nobody
# asked for. Repairing an existing directory is safe whatever the path; CREATING
# one is the operation that silently succeeds in the wrong place.
refuse_to_guess() {
  if [[ -n "$SECRETS_DIR_WAS_CHOSEN" || -d "$SECRETS_DIR" ]]; then
    return
  fi
  cat >&2 <<GUESS
$(basename "$0"): refusing to create ${SECRETS_DIR}

AUTH_SECRETS_DIR is not set and ${SECRETS_DIR} does not exist, so this would
create a secrets directory at a path nothing was asked to use — and \`sudo\`
strips the environment, which is the usual reason it is unset here.

Pass it explicitly:

  sudo AUTH_SECRETS_DIR=/home/infra/auth-state/secrets $0 $*

GUESS
  exit 2
}

cmd_fix() {
  require_root
  refuse_to_guess "$@"

  mkdir -p "$SECRETS_DIR"
  chown "${SERVICE_UID}:${SERVICE_GID}" "$SECRETS_DIR"
  chmod 700 "$SECRETS_DIR"

  local file
  while IFS= read -r -d '' file; do
    chown "${SERVICE_UID}:${SERVICE_GID}" "$file"
    chmod 400 "$file"
  done < <(find "$SECRETS_DIR" -maxdepth 1 -type f -print0)

  echo "secrets: ${SECRETS_DIR} and its contents are owned by ${SERVICE_UID}:${SERVICE_GID}"
  ls -la "$SECRETS_DIR"
}

cmd_metrics_token() {
  require_root
  refuse_to_guess "$@"

  local token_file="${SECRETS_DIR}/metrics-token"

  if [[ -s "$token_file" ]]; then
    echo "secrets: metrics-token already exists, kept"
  else
    # Generated here, never on a laptop. Base64 with the punctuation removed so
    # it survives an Authorization header, a YAML scalar and a shell variable
    # without quoting rules mattering.
    openssl rand -base64 48 | tr -d '/+=\n' | head -c 48 > "$token_file"
    echo "secrets: metrics-token generated"
  fi

  cmd_fix >/dev/null
  echo "secrets: written to ${token_file}"
}

cmd_show() {
  require_root
  local name="${1:?usage: show <name>}"
  cat "${SECRETS_DIR}/${name}"
  echo
}

case "${1:-}" in
  fix)           cmd_fix ;;
  metrics-token) cmd_metrics_token ;;
  show)          shift; cmd_show "$@" ;;
  *)             usage ;;
esac
