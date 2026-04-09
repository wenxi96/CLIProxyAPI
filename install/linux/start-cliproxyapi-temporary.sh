#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-${SCRIPT_DIR}}"
BINARY_PATH="${INSTALL_DIR}/cli-proxy-api"
CONFIG_PATH="${INSTALL_DIR}/config.yaml"
PID_FILE="${INSTALL_DIR}/run/cliproxyapi.pid"
LOG_FILE="${INSTALL_DIR}/logs/main.log"
SERVICE_NAME="${SERVICE_NAME:-cliproxyapi}"
ALT_SERVICE_NAME="${ALT_SERVICE_NAME:-cli-proxy-api}"
STOP_SCRIPT="${INSTALL_DIR}/stop-cliproxyapi.sh"
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
  bash start-cliproxyapi-temporary.sh [--yes]

说明：
  以临时前台外守护模式启动 CLIProxyAPI，并写入 PID 与日志文件。
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

confirm_stop_conflicts() {
  local answer=""
  if [[ "${ASSUME_YES}" == "true" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    return 1
  fi

  while true; do
    read -r -p "检测到已有运行实例，是否先停止后再启动临时服务？ [Y/n]: " answer
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
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid="$(cat "${PID_FILE}" 2>/dev/null || true)"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
      return 0
    fi
  fi
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

ensure_runtime_dirs() {
  mkdir -p "$(dirname "${PID_FILE}")" "$(dirname "${LOG_FILE}")"
}

[[ -x "${BINARY_PATH}" ]] || { log_error "未找到可执行文件：${BINARY_PATH}"; exit 1; }
[[ -f "${CONFIG_PATH}" ]] || { log_error "未找到配置文件：${CONFIG_PATH}"; exit 1; }

if has_conflicts; then
  log_warn "检测到系统级服务、用户级服务残留或已有临时进程。"
  if ! confirm_stop_conflicts; then
    log_warn "已取消启动临时服务。"
    exit "${CANCEL_EXIT_CODE}"
  fi
  bash "${STOP_SCRIPT}" --yes
fi

ensure_runtime_dirs
nohup "${BINARY_PATH}" -config "${CONFIG_PATH}" >>"${LOG_FILE}" 2>&1 &
echo "$!" > "${PID_FILE}"
sleep 1

if kill -0 "$(cat "${PID_FILE}")" >/dev/null 2>&1; then
  log_info "临时服务已启动。"
  log_info "PID 文件：${PID_FILE}"
  log_info "日志文件：${LOG_FILE}"
  exit 0
fi

log_error "临时服务启动失败，请检查日志：${LOG_FILE}"
exit 1
