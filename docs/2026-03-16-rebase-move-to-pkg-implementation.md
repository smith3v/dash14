# Rebase Move To Pkg Implementation Plan

**Goal:** Rebase `feat/move-to-pkg` onto `main`, keep the intended `pkg/` package layout where practical, and restore a green local build, deployment build, and test suite.

**Architecture:** Treat `main` as the source of truth for current behavior and deployment layout, then replay the branch's package move on top of it with targeted conflict resolution. After the rebase, repair imports, paths, and scripts so runtime code, Docker/deploy assets, and tests all agree on the final repository structure.

**Tech Stack:** Git, Go 1.26, GORM, Telegram bot library, Docker/Compose, shell tooling

---

### Task 1: Inspect branch delta and write the rebase strategy

**Prompt:**
Review the current branch, `main`, and the single branch-only commit to confirm the intended change is the `pkg/` move. Use `git status --short --branch`, `git log --oneline --decorate --graph --max-count=20 --all`, `git diff --name-status main...HEAD`, and `git show --stat --summary --format=fuller <branch-commit>`. Summarize which files moved, which files were added later on `main`, and which areas are likely to conflict (`cmd/dash14/main.go`, package imports, deployment/runtime paths). Do not edit application code in this step. Verify the understanding is correct before proceeding to the rebase.

---

### Task 2: Rebase the branch onto main and resolve structural conflicts

**Prompt:**
Run a non-interactive rebase of the current branch onto `main`. Resolve conflicts by preserving `main`'s newer behavior and deployment/runtime additions while applying the branch's `pkg/` package layout to the code packages. Update import paths in all affected Go files, including `cmd/dash14/main.go`, package tests, and any newly added files on `main` that still reference pre-`pkg/` paths. Keep deployment-only directories such as `deploy/` and `runtime/` at top level unless there is a concrete reason they also need to move. Confirm the rebase completes cleanly with `git status`.

---

### Task 3: Repair compilation and deployment path assumptions

**Prompt:**
Audit the rebased tree for broken imports and stale file paths. Use `rg` to find references to old package paths such as `github.com/smith3v/dash14/app`, `.../game`, `.../storage`, and similar. Fix Go imports so all packages build under the final structure. Also inspect Docker/deployment files (`Dockerfile`, `docker-compose.yml`, `DEPLOYMENT.md`, `deploy/`) and runtime/config examples for path assumptions that may have been invalidated by the move. Apply the minimal code and config changes needed to make the repository internally consistent. Add or update tests only where the rebase changed expected behavior or paths.

---

### Task 4: Run verification and close remaining gaps

**Prompt:**
Run the full verification pass with the final rebased code: `go test ./...`, a normal project build for `./cmd/dash14`, and deployment-related checks such as `docker build` if the Docker context depends on the new package layout. If a command fails due to sandbox or environment restrictions, retry with escalation when appropriate. Fix any remaining compile/test/deployment issues uncovered by verification. End with a clean summary of what changed, what was verified, and any residual risk.

---
