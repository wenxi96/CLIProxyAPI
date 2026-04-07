#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel)"
OUTPUT_PATH="${1:-release-notes.md}"
CUSTOM_NOTES_FILE="${ROOT_DIR}/.github/release-notes/custom.md"

# shellcheck source=/dev/null
source "${ROOT_DIR}/scripts/release-lib.sh"
release_load_metadata

BASE_TAG="$(release_resolve_base_tag)"
if [[ -z "${BASE_TAG}" ]]; then
  echo "failed to resolve upstream base tag from current branch" >&2
  exit 1
fi

BASE_VERSION="${BASE_TAG#v}"
SHORT_COMMIT="$(git rev-parse --short HEAD)"
FULL_COMMIT="$(git rev-parse HEAD)"
DISPLAY_VERSION="${VERSION:-$(release_resolve_display_version "${BASE_TAG}")}"
UPSTREAM_REF="upstream/${UPSTREAM_BRANCH}"

fetch_release_field() {
  local url="$1"
  local field="$2"
  python3 - "$url" "$field" <<'PY'
import json
import sys
import urllib.request

url, field = sys.argv[1], sys.argv[2]
request = urllib.request.Request(
    url,
    headers={
        "Accept": "application/vnd.github+json",
        "User-Agent": "CLIProxyAPI-ReleaseNotes",
    },
)

try:
    with urllib.request.urlopen(request, timeout=20) as response:
        payload = json.load(response)
except Exception:
    sys.exit(0)

value = payload.get(field) or ""
if isinstance(value, str):
    sys.stdout.write(value)
PY
}

fork_range="${BASE_TAG}..HEAD"
if git rev-parse --verify "${UPSTREAM_REF}" >/dev/null 2>&1; then
  fork_range="${UPSTREAM_REF}..HEAD"
fi

custom_changes="$(git log --no-merges --pretty=format:'- %h %s' "${fork_range}" || true)"
manual_notes=""
if [[ -f "${CUSTOM_NOTES_FILE}" ]]; then
  manual_notes="$(sed '/^[[:space:]]*$/d' "${CUSTOM_NOTES_FILE}" || true)"
fi

upstream_notes_url="https://api.github.com/repos/${UPSTREAM_REPO}/releases/tags/${BASE_TAG}"
upstream_notes="$(fetch_release_field "${upstream_notes_url}" body)"

{
  echo "## 我的更新"
  echo
  if [[ -n "${manual_notes}" ]]; then
    echo "### 手工说明"
    echo "${manual_notes}"
    echo
  fi
  echo "### 自定义提交"
  if [[ -n "${custom_changes}" ]]; then
    echo "${custom_changes}"
  else
    echo "- 当前版本未包含额外自定义提交。"
  fi
  echo
  echo "## 上游基线"
  echo
  echo "- 上游仓库：\`${UPSTREAM_REPO}\`"
  echo "- 上游版本：\`${BASE_TAG}\`"
  echo "- 当前版本：\`${DISPLAY_VERSION}\`"
  echo "- 当前提交：\`${FULL_COMMIT}\`"
  if [[ -n "${GITHUB_SERVER_URL:-}" && -n "${GITHUB_REPOSITORY:-}" && -n "${GITHUB_RUN_ID:-}" ]]; then
    echo "- 工作流：${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}"
  fi
  echo
  echo "## 上游更新"
  echo
  if [[ -n "${upstream_notes}" ]]; then
    echo "${upstream_notes}"
  else
    echo "- 未获取到上游 Release 说明。"
  fi
  echo
  echo "## 构建信息"
  echo
  echo "- 基线版本：\`${BASE_VERSION}\`"
  echo "- 自定义标识：\`${CUSTOM_MARK}\`"
  echo "- 自定义版本号：\`${CUSTOM_VERSION}\`"
  echo "- 构建提交：\`${SHORT_COMMIT}\`"
} > "${OUTPUT_PATH}"
