# Fork Maintainer Workflow

This fork uses a layered branch model so upstream sync and local development stay separate.

## Branch Roles

- `main`: upstream mirror branch. Keep this branch aligned with `upstream/main`.
- `master`: stable fork branch and the default branch on GitHub for this fork.
- `dev`: integration branch for upstream updates and completed feature work.
- `feature/*`: short-lived development branches created from `dev`.

## Why This Model Exists

This setup separates four concerns:

1. What upstream shipped
2. What the fork considers stable
3. What is currently being integrated
4. What is still under active development

That keeps `main` clean, avoids mixing half-finished work into stable history, and gives upstream conflict resolution a dedicated lane.

## Daily Upstream Sync

The default branch `master` contains the workflow file `.github/workflows/sync-upstream.yml`.

That workflow:

- runs every day at 09:00 Asia/Shanghai time
- supports manual `workflow_dispatch`
- syncs `origin/main` from `upstream/main`
- only allows fast-forward updates
- fails instead of overwriting fork-only commits on `main`

Important: the workflow lives on `master`, but it updates `main`.

## Recommended Flow

### 1. Let automation update `main`

In normal operation, the GitHub Actions workflow updates `origin/main` every morning.

If needed, you can also trigger `sync-upstream` manually from the GitHub Actions page.

### 2. Bring upstream changes into `dev`

```bash
git checkout dev
git pull origin dev
git merge main
```

Resolve conflicts in `dev`, not in `master`.

### 3. Start new work from `dev`

```bash
git checkout dev
git pull origin dev
git checkout -b feature/my-change
```

### 4. Merge feature work back into `dev`

```bash
git checkout dev
git merge feature/my-change
git push origin dev
```

### 5. Promote validated work to `master`

```bash
git checkout master
git pull origin master
git merge dev
git push origin master
```

## Local Sync Commands

If you want to sync the local upstream mirror branch manually:

```bash
git checkout main
git pull
git push
```

This repository is configured so that on `main`:

- `git pull` pulls from `upstream/main`
- `git push` pushes to `origin/main`

## Rules Of Thumb

- Do not develop directly on `main`.
- Do not use `master` for unfinished work.
- Do not resolve upstream conflicts in feature branches unless the conflict is feature-specific.
- Keep `feature/*` branches short-lived.
- Treat `master` as "validated fork state", not "latest upstream state".

## Frontend Panel Fork

If you also maintain your own `Cli-Proxy-API-Management-Center` fork, keep that repository on the same `main/master/dev/feature/*` model and point `remote-management.panel-github-repository` at your fork.

Recommended default:

```yaml
remote-management:
  panel-github-repository: "https://github.com/920293630/Cli-Proxy-API-Management-Center"
```

That keeps `/management.html` sourced from your own frontend release pipeline instead of the upstream panel repository.
