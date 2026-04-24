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
  local ref="${1:-HEAD}"

  git tag --merged "${ref}" --list 'v*' --sort=-version:refname \
    | grep -E "${UPSTREAM_TAG_REGEX}" \
    | head -n1 \
    || true
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

release_compare_release_order() {
  local left_custom="$1"
  local left_base="$2"
  local right_custom="$3"
  local right_base="$4"
  local comparison

  comparison="$(release_compare_versions "${left_custom}" "${right_custom}")"
  if [[ "${comparison}" != "0" ]]; then
    printf '%s' "${comparison}"
    return 0
  fi

  release_compare_versions "${left_base}" "${right_base}"
}

release_increment_minor_version() {
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

release_increment_major_version() {
  local raw="$1"
  local parts first_value index

  IFS='.' read -r -a parts <<< "${raw}"
  first_value="${parts[0]}"
  parts[0]="$((10#${first_value} + 1))"
  for ((index = 1; index < ${#parts[@]}; index += 1)); do
    parts[${index}]=0
  done

  (
    IFS='.'
    printf '%s' "${parts[*]}"
  )
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
    *-"${CUSTOM_MARK}"-*)
      printf '%s' "${raw#*-${CUSTOM_MARK}-}"
      ;;
    *-"${CUSTOM_MARK}".*)
      printf '%s' "${raw#*-${CUSTOM_MARK}.}"
      ;;
    *)
      return 1
      ;;
  esac
}

release_extract_base_version() {
  local raw="$1"

  raw="$(release_normalize_version_value "${raw}")"
  case "${raw}" in
    *-"${CUSTOM_MARK}"-*)
      printf '%s' "${raw%%-${CUSTOM_MARK}-*}"
      ;;
    *-"${CUSTOM_MARK}".*)
      printf '%s' "${raw%%-${CUSTOM_MARK}.*}"
      ;;
    *)
      return 1
      ;;
  esac
}

release_is_official_fork_release_tag() {
  local tag="$1"
  local normalized custom_version base_version

  if [[ "${tag}" == *-build.* ]]; then
    return 1
  fi

  normalized="$(release_normalize_version_value "${tag}")"

  custom_version="$(release_extract_fork_version "${normalized}" 2>/dev/null || true)"
  base_version="$(release_extract_base_version "${normalized}" 2>/dev/null || true)"
  [[ -n "${custom_version}" && -n "${base_version}" ]] || return 1
  [[ "${custom_version}" =~ ^[0-9]+([.][0-9]+)*$ ]]
}

release_list_fork_release_records() {
  local ref="${1:-HEAD}"
  local tag custom_version base_version

  git tag --merged "${ref}" --list 'v*' \
    | while IFS= read -r tag; do
        [[ -n "${tag}" ]] || continue
        if ! release_is_official_fork_release_tag "${tag}"; then
          continue
        fi

        custom_version="$(release_extract_fork_version "${tag}")"
        base_version="$(release_extract_base_version "${tag}")"
        printf '%s|%s|%s\n' "${custom_version}" "${base_version}" "${tag}"
      done \
    | sort -t'|' -k1,1V -k2,2V
}

release_list_fork_release_tags() {
  local ref="${1:-HEAD}"

  release_list_fork_release_records "${ref}" | while IFS='|' read -r _ _ tag; do
    [[ -n "${tag}" ]] || continue
    printf '%s\n' "${tag}"
  done
}

release_resolve_latest_fork_release_tag() {
  local ref="${1:-HEAD}"

  release_list_fork_release_records "${ref}" | tail -n1 | awk -F'|' '{print $3}' || true
}

release_resolve_previous_fork_release_tag() {
  local current_version="$1"
  local ref="${2:-HEAD}"
  local current_custom_version current_base_version
  local custom_version base_version tag comparison candidate_tag=""

  current_custom_version="$(release_extract_fork_version "${current_version}")" || return 0
  current_base_version="$(release_extract_base_version "${current_version}")" || return 0

  while IFS='|' read -r custom_version base_version tag; do
    [[ -n "${tag}" ]] || continue
    comparison="$(release_compare_release_order "${custom_version}" "${base_version}" "${current_custom_version}" "${current_base_version}")"
    if [[ "${comparison}" == "-1" ]]; then
      candidate_tag="${tag}"
    fi
  done < <(release_list_fork_release_records "${ref}")

  if [[ -n "${candidate_tag}" ]]; then
    printf '%s\n' "${candidate_tag}"
  fi
}

release_detect_bump_mode() {
  local raw="${RELEASE_BUMP:-${CUSTOM_BUMP:-auto}}"

  raw="$(printf '%s' "${raw}" | tr '[:upper:]' '[:lower:]')"
  case "${raw}" in
    auto|major|minor|preserve)
      printf '%s' "${raw}"
      ;;
    *)
      printf 'auto'
      ;;
  esac
}

release_has_custom_changes_since() {
  local previous_tag="$1"
  local upstream_ref="upstream/${UPSTREAM_BRANCH}"

  if [[ -z "${previous_tag}" ]]; then
    return 1
  fi

  if git rev-parse --verify "${upstream_ref}" >/dev/null 2>&1; then
    git log --no-merges --format='%H' "${previous_tag}..HEAD" --not "${upstream_ref}" | grep -q .
    return $?
  fi

  git log --no-merges --format='%H' "${previous_tag}..HEAD" | grep -q .
}

release_format_display_version() {
  local base_version="$1"
  local custom_version="$2"

  base_version="${base_version#v}"
  printf '%s-%s-%s' "${base_version}" "${CUSTOM_MARK}" "${custom_version}"
}

release_resolve_effective_custom_version() {
  local base_tag="$1"
  local base_version latest_tag latest_version latest_base_version bump_mode

  base_version="${base_tag#v}"
  latest_tag="$(release_resolve_latest_fork_release_tag)"
  if [[ -z "${latest_tag}" ]]; then
    printf '%s' "${CUSTOM_VERSION}"
    return 0
  fi

  latest_version="$(release_extract_fork_version "${latest_tag}")"
  latest_base_version="$(release_extract_base_version "${latest_tag}")"
  bump_mode="$(release_detect_bump_mode)"

  case "${bump_mode}" in
    major)
      printf '%s' "$(release_increment_major_version "${latest_version}")"
      return 0
      ;;
    minor)
      printf '%s' "$(release_increment_minor_version "${latest_version}")"
      return 0
      ;;
    preserve)
      printf '%s' "${latest_version}"
      return 0
      ;;
  esac

  if release_has_custom_changes_since "${latest_tag}"; then
    printf '%s' "$(release_increment_minor_version "${latest_version}")"
    return 0
  fi

  if [[ "$(release_compare_versions "${base_version}" "${latest_base_version}")" == "1" ]]; then
    printf '%s' "${latest_version}"
    return 0
  fi

  printf '%s' "${latest_version}"
}

release_resolve_display_version() {
  local base_tag="$1"
  local effective_custom_version

  effective_custom_version="$(release_resolve_effective_custom_version "${base_tag}")"
  release_format_display_version "${base_tag#v}" "${effective_custom_version}"
}

release_resolve_snapshot_version() {
  local display_version="$1"
  printf '%s' "${display_version}"
}

release_resolve_snapshot_tag() {
  local snapshot_version="$1"
  printf 'v%s' "${snapshot_version}"
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
