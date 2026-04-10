#!/usr/bin/env bash

release_root_dir() {
  git rev-parse --show-toplevel
}

release_load_metadata() {
  local metadata_file key value

  RELEASE_ROOT_DIR="$(release_root_dir)"
  CUSTOM_MARK="wx"
  CUSTOM_VERSION="1.0"
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

release_list_fork_release_tags() {
  local base_tag="$1"
  local base_version="${base_tag#v}"
  local prefix="v${base_version}-${CUSTOM_MARK}."
  local tag suffix

  git tag --merged HEAD --list "${prefix}*" --sort=-version:refname \
    | while IFS= read -r tag; do
        [[ -n "${tag}" ]] || continue
        suffix="${tag#${prefix}}"
        [[ "${suffix}" =~ ^[0-9]+([.][0-9]+)*$ ]] || continue
        printf '%s\n' "${tag}"
      done
}

release_resolve_latest_fork_release_tag() {
  local base_tag="$1"
  release_list_fork_release_tags "${base_tag}" | head -n1 || true
}

release_resolve_previous_fork_release_tag() {
  local base_tag="$1"
  local current_version="$2"
  local current_custom_version tag tag_version comparison

  current_custom_version="$(release_extract_fork_version "${current_version}")" || return 0
  release_list_fork_release_tags "${base_tag}" \
    | while IFS= read -r tag; do
        [[ -n "${tag}" ]] || continue
        tag_version="$(release_extract_fork_version "${tag}")"
        comparison="$(release_compare_versions "${tag_version}" "${current_custom_version}")"
        if [[ "${comparison}" == "-1" ]]; then
          printf '%s\n' "${tag}"
          break
        fi
      done
}

release_compare_versions() {
  local left="$1"
  local right="$2"
  local first

  if [[ "${left}" == "${right}" ]]; then
    printf '0'
    return 0
  fi

  first="$(printf '%s\n%s\n' "${left}" "${right}" | sort -V | head -n1)"
  if [[ "${first}" == "${left}" ]]; then
    printf -- '-1'
    return 0
  fi

  printf '1'
}

release_increment_version() {
  local raw="$1"
  local parts last_index last_value

  IFS='.' read -r -a parts <<< "${raw}"
  last_index=$((${#parts[@]} - 1))
  last_value="${parts[${last_index}]}"
  parts[${last_index}]="$((10#${last_value} + 1))"

  (
    IFS='.'
    printf '%s' "${parts[*]}"
  )
}

release_resolve_effective_custom_version() {
  local base_tag="$1"
  local latest_tag latest_version next_version comparison

  latest_tag="$(release_resolve_latest_fork_release_tag "${base_tag}")"
  if [[ -z "${latest_tag}" ]]; then
    printf '%s' "${CUSTOM_VERSION}"
    return 0
  fi

  latest_version="$(release_extract_fork_version "${latest_tag}")"
  next_version="$(release_increment_version "${latest_version}")"
  comparison="$(release_compare_versions "${next_version}" "${CUSTOM_VERSION}")"

  if [[ "${comparison}" == "-1" ]]; then
    printf '%s' "${CUSTOM_VERSION}"
    return 0
  fi

  printf '%s' "${next_version}"
}

release_resolve_display_version() {
  local base_tag="$1"
  local base_version="${base_tag#v}"
  local effective_custom_version

  effective_custom_version="$(release_resolve_effective_custom_version "${base_tag}")"
  printf '%s-%s.%s' "${base_version}" "${CUSTOM_MARK}" "${effective_custom_version}"
}

release_resolve_snapshot_version() {
  local display_version="$1"
  local short_commit="$2"
  printf '%s-build.%s' "${display_version}" "${short_commit}"
}

release_resolve_snapshot_tag() {
  local snapshot_version="$1"
  printf 'v%s' "${snapshot_version}"
}

release_normalize_version_value() {
  local raw="$1"
  raw="${raw#v}"
  raw="$(printf '%s' "${raw}" | sed -E 's/-build\.[0-9a-f]+$//')"
  printf '%s' "${raw}"
}

release_extract_fork_version() {
  local raw="$1"

  raw="$(release_normalize_version_value "${raw}")"
  case "${raw}" in
    *-"${CUSTOM_MARK}".*)
      printf '%s' "${raw#*-${CUSTOM_MARK}.}"
      ;;
    *)
      return 1
      ;;
  esac
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
