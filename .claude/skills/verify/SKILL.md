---
name: verify
description: Prove neomd builds, passes unit tests, runs integration tests against GreenMail, and verifies compiled CLI runtime.
user_invocable: true
---

# Verify neomd

Use /verify to run scripts/verify.sh from the repository root on a feature
branch with a clean worktree. The script captures the complete run in
evidence/verify.log; that directory is intentionally ignored by git.

The verification gates are:

- repository preconditions: repository root, non-default feature branch, and a
  clean worktree;
- code hygiene: whitespace-error and merge-marker checks;
- build quality: go vet ./..., go test ./..., and a trimmed compiled binary;
- integration and hardening tests: an ephemeral GreenMail 2.1.0 container when
  Docker or Podman is available, or the host named by NEOMD_TEST_IMAP_HOST;
  if neither is available, the log records that integration was skipped;
- compiled CLI runtime: neomd --version format and usable neomd --help;
- credential safety: the evidence log is scanned for common credential and
  private-key patterns; and
- evidence capture: all output is written to evidence/verify.log and a
  successful run ends with the evidence path.

Do not run this skill on main or master, and do not commit the generated
evidence log.
