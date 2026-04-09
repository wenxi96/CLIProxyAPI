#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-${SCRIPT_DIR}}"
SERVICE_NAME="${SERVICE_NAME:-cliproxyapi}"
REPO_OWNER="${REPO_OWNER:-wenxi96}"
REPO_NAME="${REPO_NAME:-CLIProxyAPI}"
REPO_BRANCH="${REPO_BRANCH:-master}"
INSTALLER_URL="${INSTALLER_URL:-https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/refs/heads/${REPO_BRANCH}/install/linux/cliproxyapi-installer.sh}"
BIN_PATH="${INSTALL_DIR}/cli-proxy-api"

log() {
  printf '[%s] %s\n' "$1" "$2"
}

show_help() {
  cat <<EOF
用法：
  bash update-cliproxyapi-safe.sh

说明：
  停止当前系统级服务，调用 fork 安装器执行 update，再按原运行状态恢复服务。

支持环境变量：
  INSTALL_DIR
  SERVICE_NAME
  REPO_OWNER / REPO_NAME / REPO_BRANCH
  INSTALLER_URL
EOF
}

case "${1:-}" in
  -h|--help|help)
    show_help
    exit 0
    ;;
esac

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    log "ERROR" "缺少命令：$1"
    exit 1
  fi
}

run_with_sudo_if_needed() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
    return $?
  fi
  need_cmd sudo
  sudo "$@"
}

wait_process_exit() {
  local max_tries=30
  local try
  for ((try = 1; try <= max_tries; try += 1)); do
    if ! pgrep -f "^${BIN_PATH}([[:space:]].*)?$" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  log "ERROR" "更新前仍有残留进程未退出：${BIN_PATH}"
  pgrep -af "^${BIN_PATH}([[:space:]].*)?$" || true
  exit 1
}

need_cmd bash
need_cmd systemctl
need_cmd pgrep

service_was_active="false"
if systemctl is-active --quiet "${SERVICE_NAME}.service" 2>/dev/null || systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
  service_was_active="true"
  log "STEP" "停止系统级服务：${SERVICE_NAME}"
  run_with_sudo_if_needed systemctl stop "${SERVICE_NAME}"
else
  log "INFO" "服务当前未运行：${SERVICE_NAME}"
fi

log "STEP" "等待进程退出"
wait_process_exit

log "STEP" "执行安装器更新"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "${INSTALLER_URL}" | env \
    INSTALL_DIR="${INSTALL_DIR}" \
    SERVICE_NAME="${SERVICE_NAME}" \
    REPO_OWNER="${REPO_OWNER}" \
    REPO_NAME="${REPO_NAME}" \
    REPO_BRANCH="${REPO_BRANCH}" \
    bash -s -- update
else
  wget -qO- "${INSTALLER_URL}" | env \
    INSTALL_DIR="${INSTALL_DIR}" \
    SERVICE_NAME="${SERVICE_NAME}" \
    REPO_OWNER="${REPO_OWNER}" \
    REPO_NAME="${REPO_NAME}" \
    REPO_BRANCH="${REPO_BRANCH}" \
    bash -s -- update
fi

if [[ "${service_was_active}" == "true" ]]; then
  log "STEP" "恢复系统级服务：${SERVICE_NAME}"
  run_with_sudo_if_needed systemctl start "${SERVICE_NAME}"
  run_with_sudo_if_needed systemctl status "${SERVICE_NAME}" --no-pager | sed -n '1,15p'
else
  log "INFO" "服务原本未运行，本次更新后保持停止状态。"
fi

log "SUCCESS" "更新流程完成"
