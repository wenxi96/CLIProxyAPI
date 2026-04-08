#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-snapshot}"
INPUT_TAG="${2:-}"

ROOT_DIR="$(git rev-parse --show-toplevel)"
cd "${ROOT_DIR}"

# shellcheck source=/dev/null
source "${ROOT_DIR}/scripts/release-lib.sh"
release_load_metadata

emit() {
  printf '%s=%s\n' "$1" "$2"
}

SHORT_COMMIT="$(git rev-parse --short HEAD)"
FULL_COMMIT="$(git rev-parse HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
SOURCE_REPOSITORY="$(release_resolve_source_repository)"

case "${MODE}" in
  snapshot)
    BASE_TAG="$(release_resolve_base_tag)"
    if [[ -z "${BASE_TAG}" ]]; then
      echo "failed to resolve upstream base tag from current branch" >&2
      exit 1
    fi

    BASE_VERSION="${BASE_TAG#v}"
    VERSION="$(release_resolve_display_version "${BASE_TAG}")"
    SNAPSHOT_TAG="$(release_resolve_snapshot_tag "${VERSION}" "${SHORT_COMMIT}")"
    SNAPSHOT_NAME="${VERSION}"

    emit "MODE" "${MODE}"
    emit "BASE_TAG" "${BASE_TAG}"
    emit "BASE_VERSION" "${BASE_VERSION}"
    emit "CUSTOM_MARK" "${CUSTOM_MARK}"
    emit "CUSTOM_VERSION" "${CUSTOM_VERSION}"
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

    VERSION="$(release_normalize_version_value "${RELEASE_TAG}")"
    emit "MODE" "${MODE}"
    emit "RELEASE_TAG" "${RELEASE_TAG}"
    emit "RELEASE_NAME" "${VERSION}"
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
if [[ -n "${SOURCE_REPOSITORY}" ]]; then
  emit "SOURCE_REPOSITORY" "${SOURCE_REPOSITORY}"
fi
