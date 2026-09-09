#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "verification failed: not inside a git repository" >&2
  exit 1
}

die() {
  echo "verification failed: $*" >&2
  exit 1
}

[[ "$PWD" == "$ROOT" ]] || die "run from repo root: expected $ROOT, got $PWD"

branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null)" || die "detached HEAD; use a feature branch"
case "$branch" in
  main|master) die "refusing to verify the default branch ($branch)" ;;
esac

[[ -z "$(git status --porcelain)" ]] || die "worktree must be clean before verification"

mkdir -p "$ROOT/evidence"
git check-ignore -q "$ROOT/evidence/verify.log" || die "evidence/verify.log must be gitignored"
LOG="$ROOT/evidence/verify.log"
: > "$LOG"
exec > >(tee -a "$LOG") 2>&1

echo "neomd verification"
echo "root: $ROOT"
echo "branch: $branch"
echo "started: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"

run() {
  echo
  echo "+ $*"
  if "$@"; then
    return 0
  else
    local status=$?
    echo "command failed with exit status $status"
    exit "$status"
  fi
}

run_redacted() {
  echo
  echo "+ $1 (arguments redacted)"
  if "$@"; then
    return 0
  else
    local status=$?
    echo "command failed with exit status $status"
    exit "$status"
  fi
}

echo
echo "== code hygiene =="
run git diff --check HEAD --
markers=""
if markers="$(git grep -I -n -E '^<<<<<<< |^>>>>>>> ' -- . 2>/dev/null)"; then
  echo "$markers"
  die "merge markers found"
else
  grep_status=$?
  ((grep_status == 1)) || die "could not scan for merge markers"
fi
echo "no whitespace errors or merge markers"

echo
echo "== build and unit tests =="
run go vet ./...
run go test ./...

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/neomd-verify.XXXXXX")"
container_runtime=""
container_name=""
cleanup() {
  if [[ -n "$container_name" ]]; then
    echo
    echo "== cleanup =="
    "$container_runtime" rm -f "$container_name" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

run go build -trimpath -o "$TMP_DIR/neomd" ./cmd/neomd

echo
echo "== integration tests =="
if [[ -n "${NEOMD_TEST_IMAP_HOST:-}" ]]; then
  echo "using provided integration host: $NEOMD_TEST_IMAP_HOST"
  run go test ./internal/ -run 'TestIntegration|Integration_Hardening' -v -count=1 -timeout 180s
else
  if command -v docker >/dev/null 2>&1; then
    container_runtime=docker
  elif command -v podman >/dev/null 2>&1; then
    container_runtime=podman
  fi

  if [[ -n "$container_runtime" ]]; then
    container_name="neomd-greenmail-verify-$$"
    run_redacted "$container_runtime" run --rm -d \
      --name "$container_name" \
      -p 3993:3993 -p 3465:3465 \
      -e 'GREENMAIL_OPTS=-Dgreenmail.setup.test.all -Dgreenmail.hostname=0.0.0.0 -Dgreenmail.users=demo:demo123@neomd.local -Dgreenmail.users.login=email -Dgreenmail.verbose' \
      greenmail/standalone:2.1.0

    ready=0
    for _ in {1..60}; do
      if (echo >/dev/tcp/127.0.0.1/3993) >/dev/null 2>&1 && \
         (echo >/dev/tcp/127.0.0.1/3465) >/dev/null 2>&1; then
        ready=1
        break
      fi
      sleep 1
    done
    ((ready == 1)) || die "GreenMail did not become ready on ports 3993/3465"
    run make -s test-integration-ci
  else
    echo "NOTICE: no docker/podman runtime and NEOMD_TEST_IMAP_HOST is unset; integration tests skipped"
  fi
fi

echo
echo "== compiled CLI smoke =="
version_output="$("$TMP_DIR/neomd" --version 2>&1)" || die "compiled binary --version failed"
echo "$version_output"
[[ "$version_output" =~ ^neomd[[:space:]][^[:space:]]+$ ]] || die "unexpected --version output"

help_output="$("$TMP_DIR/neomd" --help 2>&1)" || die "compiled binary --help failed"
echo "$help_output"
[[ "$help_output" == *"-config"* ]] || die "--help output is missing expected flags"

echo
echo "== credential and private-key scan =="
if grep -Eiq \
  -- '(-----BEGIN ([A-Z0-9 ]+ )?PRIVATE KEY-----|\bAKIA[0-9A-Z]{16}\b|\bgh[pousr]_[A-Za-z0-9_]{20,}\b|\b(password|passwd|secret|token|api[_-]?key|authorization)[[:space:]]*[:=][[:space:]]*[^[:space:]]+)' \
  "$LOG"; then
  die "credential-like content found in $LOG"
fi
echo "no common credential patterns or private-key markers found"

echo
echo "verification passed; evidence: evidence/verify.log"
