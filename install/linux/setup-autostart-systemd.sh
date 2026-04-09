#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${INSTALL_DIR:-${SCRIPT_DIR}}"
SERVICE_NAME="${SERVICE_NAME:-cliproxyapi}"
BINARY_PATH="${INSTALL_DIR}/cli-proxy-api"
TARGET_USER="${TARGET_USER:-$(stat -c '%U' "${INSTALL_DIR}" 2>/dev/null || id -un)}"
TARGET_GROUP="${TARGET_GROUP:-$(id -gn "${TARGET_USER}" 2>/dev/null || id -gn)}"
TARGET_HOME="${TARGET_HOME:-$(getent passwd "${TARGET_USER}" 2>/dev/null | cut -d: -f6)}"
TARGET_HOME="${TARGET_HOME:-$HOME}"
UNIT_DST="/etc/systemd/system/${SERVICE_NAME}.service"

case "${1:-}" in
  -h|--help|help)
    cat <<EOF
用法：
  bash setup-autostart-systemd.sh

说明：
  生成并安装 ${SERVICE_NAME}.service 到 /etc/systemd/system/，然后启用开机自启。

支持环境变量：
  INSTALL_DIR
  SERVICE_NAME
  TARGET_USER / TARGET_GROUP / TARGET_HOME
EOF
    exit 0
    ;;
esac

if [[ ! -x "${BINARY_PATH}" ]]; then
  echo "ERROR: missing executable: ${BINARY_PATH}" >&2
  exit 1
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: missing command: $1" >&2
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

tmp_unit="$(mktemp)"
cat >"${tmp_unit}" <<EOF
[Unit]
Description=CLIProxyAPI Service (WSL autostart)
After=network-online.target
Wants=network-online.target
ConditionPathIsExecutable=${BINARY_PATH}

[Service]
Type=simple
User=${TARGET_USER}
Group=${TARGET_GROUP}
WorkingDirectory=${INSTALL_DIR}
ExecStart=/bin/bash -lc 'exec 0< <(tail -f /dev/null); exec ${BINARY_PATH}'
Restart=always
RestartSec=10
Environment=HOME=${TARGET_HOME}

[Install]
WantedBy=multi-user.target
EOF

run_with_sudo_if_needed install -m 0644 "${tmp_unit}" "${UNIT_DST}"
rm -f "${tmp_unit}"
run_with_sudo_if_needed systemctl daemon-reload
run_with_sudo_if_needed systemctl enable "${SERVICE_NAME}.service"

cat <<EOF
已启用 ${SERVICE_NAME}.service 开机自启。

启动服务：
  sudo systemctl start ${SERVICE_NAME}.service

检查状态：
  sudo systemctl status --no-pager ${SERVICE_NAME}.service
  sudo journalctl -u ${SERVICE_NAME}.service -n 200 --no-pager
EOF
