#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-snapshot}"
INPUT_TAG="${2:-}"
FORK_MARK="${CPA_FORK_MARK:-wx}"

ROOT_DIR="$(git rev-parse --show-toplevel)"
cd "${ROOT_DIR}"

resolve_base_tag() {
  local base_tag
  base_tag="$(
    git tag --merged HEAD --list 'v*' --sort=-version:refname \
      | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
      | head -n1 \
      || true
  )"

  if [[ -z "${base_tag}" ]]; then
    echo "failed to resolve upstream base tag from current branch" >&2
    exit 1
  fi

  printf '%s' "${base_tag}"
}

emit() {
  printf '%s=%s\n' "$1" "$2"
}

SHORT_COMMIT="$(git rev-parse --short HEAD)"
FULL_COMMIT="$(git rev-parse HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

case "${MODE}" in
  snapshot)
    BASE_TAG="$(resolve_base_tag)"
    BASE_VERSION="${BASE_TAG#v}"
    VERSION="${BASE_VERSION}-${FORK_MARK}.dev.${SHORT_COMMIT}"
    SNAPSHOT_TAG="v${VERSION}"
    SNAPSHOT_NAME="snapshot-${VERSION}"

    emit "MODE" "${MODE}"
    emit "BASE_TAG" "${BASE_TAG}"
    emit "BASE_VERSION" "${BASE_VERSION}"
    emit "VERSION" "${VERSION}"
    emit "SNAPSHOT_TAG" "${SNAPSHOT_TAG}"
    emit "SNAPSHOT_NAME" "${SNAPSHOT_NAME}"
    ;;
  release)
    RELEASE_TAG="${INPUT_TAG}"
    if [[ -z "${RELEASE_TAG}" ]]; then
      RELEASE_TAG="${GITHUB_REF_NAME:-}"
    fi
    if [[ -z "${RELEASE_TAG}" ]]; then
      RELEASE_TAG="$(git describe --tags --exact-match 2>/dev/null || true)"
    fi
    if [[ -z "${RELEASE_TAG}" ]]; then
      echo "failed to resolve release tag" >&2
      exit 1
    fi

    VERSION="${RELEASE_TAG#v}"
    emit "MODE" "${MODE}"
    emit "RELEASE_TAG" "${RELEASE_TAG}"
    emit "RELEASE_NAME" "${RELEASE_TAG}"
    emit "VERSION" "${VERSION}"
    ;;
  *)
    echo "unsupported mode: ${MODE}" >&2
    exit 1
    ;;
esac

emit "COMMIT" "${SHORT_COMMIT}"
emit "FULL_COMMIT" "${FULL_COMMIT}"
emit "BUILD_DATE" "${BUILD_DATE}"
