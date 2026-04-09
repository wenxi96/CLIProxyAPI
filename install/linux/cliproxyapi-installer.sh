#!/usr/bin/env bash
set -euo pipefail

COMMAND="${1:-install}"
SCRIPT_SOURCE="${BASH_SOURCE[0]:-}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${SCRIPT_SOURCE}")" && pwd)"

REPO_OWNER="${REPO_OWNER:-wenxi96}"
REPO_NAME="${REPO_NAME:-CLIProxyAPI}"
REPO_BRANCH="${REPO_BRANCH:-master}"
TARGET_USER="${TARGET_USER:-${SUDO_USER:-$(id -un)}}"
TARGET_GROUP="${TARGET_GROUP:-$(id -gn "${TARGET_USER}" 2>/dev/null || id -gn)}"
TARGET_HOME="${TARGET_HOME:-$(getent passwd "${TARGET_USER}" 2>/dev/null | cut -d: -f6)}"
TARGET_HOME="${TARGET_HOME:-$HOME}"
INSTALL_DIR="${INSTALL_DIR:-${TARGET_HOME}/cliproxyapi}"
SERVICE_NAME="${SERVICE_NAME:-cliproxyapi}"
ALT_SERVICE_NAME="${ALT_SERVICE_NAME:-cli-proxy-api}"
PANEL_GITHUB_REPOSITORY="${PANEL_GITHUB_REPOSITORY:-https://github.com/wenxi96/Cli-Proxy-API-Management-Center}"
RELEASE_API_URL="${RELEASE_API_URL:-https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest}"
RAW_BASE_URL="${RAW_BASE_URL:-https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/refs/heads/${REPO_BRANCH}/install/linux}"
INSTALLER_URL="${INSTALLER_URL:-${RAW_BASE_URL}/cliproxyapi-installer.sh}"

log_info() {
  printf '[INFO] %s\n' "$1"
}

log_warn() {
  printf '[WARN] %s\n' "$1" >&2
}

log_error() {
  printf '[ERROR] %s\n' "$1" >&2
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log_error "缺少命令：$1"
    exit 1
  fi
}

download_text() {
  local source="$1"
  if [[ -f "$source" ]]; then
    cat "$source"
    return 0
  fi
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$source"
    return 0
  fi
  wget -qO- "$source"
}

download_file() {
  local source="$1"
  local destination="$2"
  if [[ -f "$source" ]]; then
    install -m 0644 "$source" "$destination"
    return 0
  fi
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$source" -o "$destination"
    return 0
  fi
  wget -qO "$destination" "$source"
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf '%s' "amd64" ;;
    aarch64|arm64) printf '%s' "arm64" ;;
    *)
      log_error "当前架构暂不支持：$(uname -m)"
      exit 1
      ;;
  esac
}

archive_regex() {
  printf 'CLIProxyAPI_[^"]*_linux_%s\.tar\.gz' "$(detect_arch)"
}

fetch_release_json() {
  need_cmd grep
  need_cmd cut
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${RELEASE_API_URL}"
  else
    wget -qO- "${RELEASE_API_URL}"
  fi
}

extract_release_tag() {
  local release_json="$1"
  printf '%s' "$release_json" \
    | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' \
    | head -n 1 \
    | cut -d'"' -f4
}

extract_download_url() {
  local release_json="$1"
  local regex
  regex="$(archive_regex)"
  printf '%s' "$release_json" \
    | grep -o "\"browser_download_url\"[[:space:]]*:[[:space:]]*\"[^\"]*${regex}\"" \
    | head -n 1 \
    | cut -d'"' -f4
}

config_path() {
  printf '%s' "${INSTALL_DIR}/config.yaml"
}

binary_path() {
  printf '%s' "${INSTALL_DIR}/cli-proxy-api"
}

version_file() {
  printf '%s' "${INSTALL_DIR}/version.txt"
}

helper_script_path() {
  printf '%s/%s' "${INSTALL_DIR}" "$1"
}

local_helper_source() {
  local script_name="$1"
  local candidate="${SCRIPT_DIR}/${script_name}"
  if [[ -f "$candidate" ]]; then
    printf '%s' "$candidate"
    return 0
  fi
  printf '%s' "${RAW_BASE_URL}/${script_name}"
}

write_helper_scripts() {
  local script_name source_path destination
  mkdir -p "${INSTALL_DIR}"
  for script_name in \
    cliproxyapi-installer.sh \
    update-cliproxyapi-safe.sh \
    setup-autostart-systemd.sh \
    start-cliproxyapi-system.sh \
    start-cliproxyapi-temporary.sh \
    stop-cliproxyapi.sh; do
    source_path="$(local_helper_source "${script_name}")"
    destination="$(helper_script_path "${script_name}")"
    download_file "${source_path}" "${destination}"
    chmod 0755 "${destination}"
  done
}

generate_api_key() {
  local random_part
  random_part="$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n' | cut -c1-45)"
  printf 'sk-%s' "${random_part}"
}

ensure_runtime_dirs() {
  mkdir -p "${INSTALL_DIR}/logs" "${INSTALL_DIR}/run"
}

ensure_panel_repo_default() {
  local config_file="$1"
  local desired="${PANEL_GITHUB_REPOSITORY}"
  local current=""
  local tmp_file=""

  [[ -f "${config_file}" ]] || return 0

  current="$(grep -E '^[[:space:]]*panel-github-repository:' "${config_file}" | head -n 1 | sed -E 's/^[^:]+:[[:space:]]*"?([^"]*)"?/\1/' || true)"
  if [[ -n "${current}" ]] \
    && [[ "${current}" != "https://github.com/router-for-me/Cli-Proxy-API-Management-Center" ]] \
    && [[ "${current}" != "https://github.com/920293630/Cli-Proxy-API-Management-Center" ]] \
    && [[ "${current}" != "https://github.com/wenxi96/Cli-Proxy-API-Management-Center" ]]; then
    return 0
  fi

  tmp_file="$(mktemp)"
  if grep -Eq '^[[:space:]]*panel-github-repository:' "${config_file}"; then
    sed -E "s|^([[:space:]]*panel-github-repository:).*|\\1 \"${desired}\"|" "${config_file}" > "${tmp_file}"
  else
    awk -v injected="  panel-github-repository: \"${desired}\"" '
      BEGIN { inserted = 0 }
      {
        print
        if (!inserted && $0 ~ /^remote-management:[[:space:]]*$/) {
          print injected
          inserted = 1
        }
      }
    ' "${config_file}" > "${tmp_file}"
  fi
  mv "${tmp_file}" "${config_file}"
}

create_default_config() {
  local example_config="$1"
  local key1=""
  local key2=""
  local tmp_file=""

  [[ -f "${example_config}" ]] || return 1

  key1="$(generate_api_key)"
  key2="$(generate_api_key)"
  tmp_file="$(mktemp)"
  sed \
    -e "s/\"your-api-key-1\"/\"${key1}\"/g" \
    -e "s/\"your-api-key-2\"/\"${key2}\"/g" \
    -e "s/\"your-api-key-3\"/\"$(generate_api_key)\"/g" \
    "${example_config}" > "${tmp_file}"
  mv "${tmp_file}" "$(config_path)"
  ensure_panel_repo_default "$(config_path)"
  log_info "已生成默认配置文件：$(config_path)"
}

read_binary_banner() {
  if [[ ! -x "$(binary_path)" ]]; then
    return 1
  fi
  if command -v timeout >/dev/null 2>&1; then
    timeout 2 "$(binary_path)" 2>&1 | sed -n '1p' || true
    return 0
  fi
  "$(binary_path)" --version 2>/dev/null | sed -n '1p' || true
}

write_version_file() {
  local release_tag="$1"
  local banner=""
  banner="$(read_binary_banner || true)"
  if [[ -n "${banner}" ]]; then
    printf '%s\n' "${banner}" > "$(version_file)"
    return 0
  fi
  printf 'CLIProxyAPI Release: %s\n' "${release_tag#v}" > "$(version_file)"
}

show_status() {
  echo "CLIProxyAPI 安装状态"
  echo "===================="
  echo "安装目录：${INSTALL_DIR}"
  echo "仓库来源：${REPO_OWNER}/${REPO_NAME}"
  echo "Release API：${RELEASE_API_URL}"
  echo "面板仓库：${PANEL_GITHUB_REPOSITORY}"
  echo "二进制路径：$(binary_path)"
  echo "配置文件：$(config_path)"
  echo "版本文件：$(version_file)"
  if [[ -f "$(version_file)" ]]; then
    echo "当前版本：$(cat "$(version_file)")"
  else
    echo "当前版本：未安装"
  fi
  if [[ -f "$(config_path)" ]]; then
    echo "当前面板源：$(grep -E '^[[:space:]]*panel-github-repository:' "$(config_path)" | head -n 1 | sed -E 's/^[^:]+:[[:space:]]*"?([^"]*)"?/\1/')"
  fi
}

backup_config_if_needed() {
  local config_file
  local backup_path
  config_file="$(config_path)"
  if [[ ! -f "${config_file}" ]]; then
    return 0
  fi
  backup_path="${INSTALL_DIR}/config.yaml.bak.$(date +%Y%m%d-%H%M%S)"
  cp "${config_file}" "${backup_path}"
  printf '%s' "${backup_path}"
}

extract_archive() {
  local archive_path="$1"
  local extract_dir="$2"
  tar -xzf "${archive_path}" -C "${extract_dir}"
}

copy_release_payload() {
  local extract_dir="$1"
  local extracted_binary=""
  local example_config=""
  local readme_file=""
  local readme_cn=""
  local license_file=""
  local backup_file="$2"

  extracted_binary="$(find "${extract_dir}" -type f \( -name 'cli-proxy-api' -o -name 'CLIProxyAPI' \) | head -n 1)"
  example_config="$(find "${extract_dir}" -type f -name 'config.example.yaml' | head -n 1)"
  readme_file="$(find "${extract_dir}" -type f -name 'README.md' | head -n 1)"
  readme_cn="$(find "${extract_dir}" -type f -name 'README_CN.md' | head -n 1)"
  license_file="$(find "${extract_dir}" -type f -name 'LICENSE' | head -n 1)"

  if [[ -z "${extracted_binary}" ]]; then
    log_error "解压后未找到 CLIProxyAPI 可执行文件"
    exit 1
  fi

  install -m 0755 "${extracted_binary}" "$(binary_path)"
  [[ -n "${example_config}" ]] && cp "${example_config}" "${INSTALL_DIR}/config.example.yaml"
  [[ -n "${readme_file}" ]] && cp "${readme_file}" "${INSTALL_DIR}/README.md"
  [[ -n "${readme_cn}" ]] && cp "${readme_cn}" "${INSTALL_DIR}/README_CN.md"
  [[ -n "${license_file}" ]] && cp "${license_file}" "${INSTALL_DIR}/LICENSE"

  if [[ -n "${backup_file}" && -f "${backup_file}" ]]; then
    cp "${backup_file}" "$(config_path)"
    ensure_panel_repo_default "$(config_path)"
  elif [[ ! -f "$(config_path)" && -n "${example_config}" ]]; then
    create_default_config "${example_config}"
  elif [[ -f "$(config_path)" ]]; then
    ensure_panel_repo_default "$(config_path)"
  fi
}

install_or_update() {
  local release_json=""
  local release_tag=""
  local download_url=""
  local tmp_dir=""
  local archive_path=""
  local backup_file=""

  need_cmd tar
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    log_error "需要 curl 或 wget 才能下载安装包"
    exit 1
  fi

  mkdir -p "${INSTALL_DIR}"
  ensure_runtime_dirs
  write_helper_scripts

  release_json="$(fetch_release_json)"
  release_tag="$(extract_release_tag "${release_json}")"
  download_url="$(extract_download_url "${release_json}")"

  if [[ -z "${release_tag}" || -z "${download_url}" ]]; then
    log_error "无法从 latest release 解析下载地址，请检查发布资产是否完整"
    exit 1
  fi

  log_info "准备安装版本：${release_tag#v}"
  log_info "下载地址：${download_url}"

  tmp_dir="$(mktemp -d)"
  archive_path="${tmp_dir}/cliproxyapi.tar.gz"
  backup_file="$(backup_config_if_needed || true)"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${download_url}" -o "${archive_path}"
  else
    wget -qO "${archive_path}" "${download_url}"
  fi

  extract_archive "${archive_path}" "${tmp_dir}"
  copy_release_payload "${tmp_dir}" "${backup_file}"
  write_version_file "${release_tag}"
  rm -rf "${tmp_dir}"

  log_info "安装完成：$(binary_path)"
  log_info "配置文件：$(config_path)"
  log_info "更新脚本：$(helper_script_path update-cliproxyapi-safe.sh)"
}

confirm_uninstall() {
  if [[ ! -t 0 ]]; then
    log_error "卸载需要交互确认，请在交互终端执行。"
    exit 1
  fi
  local answer=""
  read -r -p "确认删除 ${INSTALL_DIR} 并移除 ${SERVICE_NAME}.service 吗？[y/N]: " answer
  case "${answer,,}" in
    y|yes) return 0 ;;
    *) log_warn "已取消卸载"; exit 0 ;;
  esac
}

run_with_sudo_if_needed() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
    return $?
  fi
  need_cmd sudo
  sudo "$@"
}

uninstall_all() {
  confirm_uninstall
  if [[ -x "$(helper_script_path stop-cliproxyapi.sh)" ]]; then
    bash "$(helper_script_path stop-cliproxyapi.sh)" --yes || true
  fi
  if command -v systemctl >/dev/null 2>&1; then
    if systemctl list-unit-files | grep -q "^${SERVICE_NAME}\.service"; then
      run_with_sudo_if_needed systemctl disable "${SERVICE_NAME}.service" >/dev/null 2>&1 || true
      run_with_sudo_if_needed rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
      run_with_sudo_if_needed systemctl daemon-reload || true
    fi
  fi
  rm -rf "${INSTALL_DIR}"
  log_info "已卸载：${INSTALL_DIR}"
}

show_help() {
  cat <<EOF
CLIProxyAPI Linux Installer

用法：
  bash cliproxyapi-installer.sh [install|update|upgrade|status|uninstall]

默认值：
  REPO_OWNER=${REPO_OWNER}
  REPO_NAME=${REPO_NAME}
  REPO_BRANCH=${REPO_BRANCH}
  INSTALL_DIR=${INSTALL_DIR}
  SERVICE_NAME=${SERVICE_NAME}

常用示例：
  curl -fsSL ${INSTALLER_URL} | bash
  curl -fsSL ${INSTALLER_URL} | bash -s -- update
  bash cliproxyapi-installer.sh status

支持环境变量覆盖：
  REPO_OWNER / REPO_NAME / REPO_BRANCH
  RELEASE_API_URL
  PANEL_GITHUB_REPOSITORY
  INSTALL_DIR
  SERVICE_NAME
EOF
}

case "${COMMAND}" in
  install|update|upgrade)
    install_or_update
    ;;
  status)
    show_status
    ;;
  uninstall)
    uninstall_all
    ;;
  -h|--help|help)
    show_help
    ;;
  *)
    log_error "不支持的命令：${COMMAND}"
    show_help
    exit 2
    ;;
esac
