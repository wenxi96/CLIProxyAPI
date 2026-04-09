#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-${SCRIPT_DIR}}"
BINARY_PATH="${INSTALL_DIR}/cli-proxy-api"
PID_FILE="${INSTALL_DIR}/run/cliproxyapi.pid"
SERVICE_NAME="${SERVICE_NAME:-cliproxyapi}"
ALT_SERVICE_NAME="${ALT_SERVICE_NAME:-cli-proxy-api}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes|-y)
      shift
      ;;
    -h|--help|help)
      cat <<EOF
用法：
  bash stop-cliproxyapi.sh [--yes]

说明：
  停止系统级服务、旧 user service 残留以及临时进程。
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

is_interactive_terminal() {
  [[ -t 0 ]] && [[ -t 1 || -t 2 ]]
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf '[Error] 缺少命令：%s\n' "$1" >&2
    exit 1
  fi
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
  printf '[Error] 当前操作需要 sudo 权限；非交互模式下无法输入密码，请在交互终端执行或预先配置 sudo 免密。\n' >&2
  return 1
}

stop_system_service() {
  local name="$1"
  if ! command -v systemctl >/dev/null 2>&1; then
    return 0
  fi
  if systemctl is-active --quiet "${name}.service" 2>/dev/null || systemctl is-active --quiet "${name}" 2>/dev/null; then
    log_info "停止系统级服务：${name}"
    run_with_sudo_if_needed systemctl stop "${name}"
  fi
}

stop_user_service() {
  local name="$1"
  if ! command -v systemctl >/dev/null 2>&1; then
    return 0
  fi
  if systemctl --user is-active --quiet "${name}.service" 2>/dev/null; then
    log_info "停止用户级服务（旧模式）：${name}.service"
    systemctl --user stop "${name}.service"
    systemctl --user disable "${name}.service" >/dev/null 2>&1 || true
  fi
}

wait_pid_exit() {
  local pid="$1"
  local try
  for ((try = 1; try <= 15; try += 1)); do
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

stop_processes() {
  local pid=""
  if [[ -f "${PID_FILE}" ]]; then
    pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
      log_info "停止临时进程：${pid}"
      kill -TERM "${pid}" >/dev/null 2>&1 || true
      wait_pid_exit "${pid}" || true
    fi
    rm -f "${PID_FILE}"
  fi

  if pgrep -f "^${BINARY_PATH}([[:space:]].*)?$" >/dev/null 2>&1; then
    log_info "清理残留进程：${BINARY_PATH}"
    pkill -TERM -f "^${BINARY_PATH}([[:space:]].*)?$" >/dev/null 2>&1 || true
  fi
}

stop_system_service "${SERVICE_NAME}"
[[ -n "${ALT_SERVICE_NAME}" ]] && stop_system_service "${ALT_SERVICE_NAME}"
stop_user_service "${SERVICE_NAME}"
[[ -n "${ALT_SERVICE_NAME}" ]] && stop_user_service "${ALT_SERVICE_NAME}"
stop_processes
log_info "停止操作已完成。"
