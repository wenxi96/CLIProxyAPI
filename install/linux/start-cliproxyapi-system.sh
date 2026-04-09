#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-${SCRIPT_DIR}}"
BINARY_PATH="${INSTALL_DIR}/cli-proxy-api"
CONFIG_PATH="${INSTALL_DIR}/config.yaml"
SERVICE_NAME="${SERVICE_NAME:-cliproxyapi}"
ALT_SERVICE_NAME="${ALT_SERVICE_NAME:-cli-proxy-api}"
STOP_SCRIPT="${INSTALL_DIR}/stop-cliproxyapi.sh"
SETUP_SCRIPT="${INSTALL_DIR}/setup-autostart-systemd.sh"
ASSUME_YES="false"
CANCEL_EXIT_CODE=200

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes|-y)
      ASSUME_YES="true"
      shift
      ;;
    -h|--help|help)
      cat <<EOF
用法：
  bash start-cliproxyapi-system.sh [--yes]

说明：
  安装并启动系统级 ${SERVICE_NAME}.service。若检测到已有临时进程或旧服务，会先提示是否停止。
EOF
      exit 0
      ;;
    *)
      echo "[ERROR] 未知参数：$1" >&2
      exit 2
      ;;
  esac
done

log_info() {
  printf '[Info] %s\n' "$1"
}

log_warn() {
  printf '[Warn] %s\n' "$1"
}

log_error() {
  printf '[Error] %s\n' "$1" >&2
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log_error "缺少命令：$1"
    exit 1
  fi
}

is_interactive_terminal() {
  [[ -t 0 ]] && [[ -t 1 || -t 2 ]]
}

run_with_sudo_if_needed() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
    return $?
  fi
  need_cmd sudo
  if sudo -n true >/dev/null 2>&1; then
    sudo "$@"
    return $?
  fi
  if is_interactive_terminal; then
    sudo "$@"
    return $?
  fi
  log_error "当前操作需要 sudo 权限；非交互模式下无法输入密码，请在交互终端执行或预先配置 sudo 免密。"
  return 1
}

confirm_stop_conflicts() {
  local answer=""

  if [[ "${ASSUME_YES}" == "true" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    return 1
  fi

  while true; do
    read -r -p "检测到已有运行实例，是否先停止后再启动系统级服务？ [Y/n]: " answer
    answer="${answer:-Y}"
    case "${answer,,}" in
      y|yes) return 0 ;;
      n|no) return 1 ;;
      *) echo "请输入 y 或 n" ;;
    esac
  done
}

service_is_active() {
  local name="$1"
  systemctl is-active --quiet "${name}.service" 2>/dev/null || systemctl is-active --quiet "$name" 2>/dev/null
}

user_service_is_active() {
  local name="$1"
  systemctl --user is-active --quiet "${name}.service" 2>/dev/null
}

binary_process_is_running() {
  pgrep -f "^${BINARY_PATH}([[:space:]].*)?$" >/dev/null 2>&1
}

has_conflicts() {
  service_is_active "${SERVICE_NAME}" && return 0
  [[ -n "${ALT_SERVICE_NAME}" ]] && service_is_active "${ALT_SERVICE_NAME}" && return 0
  user_service_is_active "${SERVICE_NAME}" && return 0
  [[ -n "${ALT_SERVICE_NAME}" ]] && user_service_is_active "${ALT_SERVICE_NAME}" && return 0
  binary_process_is_running && return 0
  return 1
}

[[ -x "${BINARY_PATH}" ]] || { log_error "未找到可执行文件：${BINARY_PATH}"; exit 1; }
[[ -f "${CONFIG_PATH}" ]] || { log_error "未找到配置文件：${CONFIG_PATH}"; exit 1; }
need_cmd systemctl

if has_conflicts; then
  log_warn "检测到系统级服务、用户级服务残留或已有临时进程。"
  if ! confirm_stop_conflicts; then
    log_warn "已取消启动系统级服务。"
    exit "${CANCEL_EXIT_CODE}"
  fi
  bash "${STOP_SCRIPT}" --yes
fi

if [[ -x "${SETUP_SCRIPT}" ]]; then
  log_info "安装并启用系统级服务：${SERVICE_NAME}"
  bash "${SETUP_SCRIPT}"
else
  log_error "未找到 setup-autostart-systemd.sh：${SETUP_SCRIPT}"
  exit 1
fi

run_with_sudo_if_needed systemctl restart "${SERVICE_NAME}"
log_info "系统级服务已启动：${SERVICE_NAME}"
run_with_sudo_if_needed systemctl status "${SERVICE_NAME}" --no-pager | sed -n '1,15p'
