#!/usr/bin/env bash

release_root_dir() {
  git rev-parse --show-toplevel
}

release_load_metadata() {
  local metadata_file key value

  RELEASE_ROOT_DIR="$(release_root_dir)"
  CUSTOM_MARK="wx"
  CUSTOM_VERSION="1"
  UPSTREAM_REPO="router-for-me/CLIProxyAPI"
  UPSTREAM_BRANCH="main"
  UPSTREAM_TAG_REGEX='^v[0-9]+\.[0-9]+\.[0-9]+$'

  metadata_file="${RELEASE_ROOT_DIR}/release-metadata.env"
  if [[ ! -f "${metadata_file}" ]]; then
    return 0
  fi

  while IFS='=' read -r key value; do
    [[ -z "${key}" ]] && continue
    [[ "${key}" == \#* ]] && continue

    case "${key}" in
      CUSTOM_MARK|CUSTOM_VERSION|UPSTREAM_REPO|UPSTREAM_BRANCH|UPSTREAM_TAG_REGEX)
        printf -v "${key}" '%s' "${value}"
        ;;
    esac
  done < "${metadata_file}"
}

release_resolve_base_tag() {
  git tag --merged HEAD --list 'v*' --sort=-version:refname \
    | grep -E "${UPSTREAM_TAG_REGEX}" \
    | head -n1 \
    || true
}

release_resolve_display_version() {
  local base_tag="$1"
  local base_version="${base_tag#v}"
  printf '%s-%s.%s' "${base_version}" "${CUSTOM_MARK}" "${CUSTOM_VERSION}"
}

release_resolve_snapshot_tag() {
  local display_version="$1"
  local short_commit="$2"
  printf 'v%s-build.%s' "${display_version}" "${short_commit}"
}

release_normalize_version_value() {
  local raw="$1"
  raw="${raw#v}"
  raw="$(printf '%s' "${raw}" | sed -E 's/-build\.[0-9a-f]+$//')"
  printf '%s' "${raw}"
}

release_resolve_source_repository() {
  local remote_url
  remote_url="$(git remote get-url origin 2>/dev/null || true)"
  remote_url="$(printf '%s' "${remote_url}" | tr -d '\r\n')"

  if [[ -z "${remote_url}" ]]; then
    return 0
  fi

  if [[ "${remote_url}" =~ ^https?://github\.com/([^/]+)/([^/]+?)(\.git)?$ ]]; then
    printf 'https://github.com/%s/%s' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]%.git}"
    return 0
  fi

  if [[ "${remote_url}" =~ ^git@github\.com:([^/]+)/([^/]+?)(\.git)?$ ]]; then
    printf 'https://github.com/%s/%s' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]%.git}"
    return 0
  fi

  if [[ "${remote_url}" =~ ^[^/]+:([^/]+)/([^/]+?)(\.git)?$ ]]; then
    printf 'https://github.com/%s/%s' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]%.git}"
    return 0
  fi

  printf '%s' "${remote_url%.git}"
}
