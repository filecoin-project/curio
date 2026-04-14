#!/usr/bin/env bash
set -Eeuo pipefail

# -----------------------------------------------------------------------------
# YugabyteDB + PostgreSQL AMI provisioning script for Ubuntu 24.04 x86_64
#
# Decisions baked into this script:
# - OS target: Ubuntu 24.04
# - Architecture: x86_64
# - YugabyteDB version: 2025.2.2.2-b11
# - PostgreSQL version: 18
# - Install path: /usr/local/yugabyte
# - Base dir: /var/lib/yugabytedb
# - User/group: yugabyte:yugabyte
# - Service name: yugabytedb.service
# - Listen only on 127.0.0.1
# - Start and validate
# -----------------------------------------------------------------------------

readonly YB_VERSION="2025.2.2.2-b11"
readonly YB_SERIES_DIR="2025.2.2.2"
readonly YB_TARBALL="yugabyte-${YB_VERSION}-linux-x86_64.tar.gz"
readonly YB_DOWNLOAD_URL="https://downloads.yugabyte.com/releases/${YB_SERIES_DIR}/${YB_TARBALL}"

readonly YB_INSTALL_ROOT="/opt/yugabyte"
readonly YB_CURRENT_LINK="${YB_INSTALL_ROOT}/current"

readonly YB_USER="yugabyte"
readonly YB_GROUP="yugabyte"
readonly YB_HOME="/home/yugabyte"
readonly YB_BASE_DIR="/var/lib/yugabytedb"
readonly YB_SERVICE_NAME="yugabytedb.service"

readonly YB_BIN_DIR="${YB_CURRENT_LINK}/bin"
readonly YB_YUGABYTED="${YB_BIN_DIR}/yugabyted"
readonly YB_YSQLSH="${YB_BIN_DIR}/ysqlsh"

readonly YB_LISTEN_IP="127.0.0.1"
readonly YB_YSQL_PORT="5433"
readonly YB_UI_PORT="15433"

readonly SYSTEMD_UNIT_PATH="/etc/systemd/system/${YB_SERVICE_NAME}"
readonly SYSCTL_CONF="/etc/sysctl.d/99-yugabytedb.conf"
readonly LIMITS_CONF="/etc/security/limits.d/99-yugabytedb.conf"

readonly PG_VERSION="18"
readonly PG_CLUSTER="main"
readonly PG_CLUSTER_UNIT="postgresql@${PG_VERSION}-${PG_CLUSTER}.service"
readonly PG_LISTEN_IP="127.0.0.1"
readonly PG_PORT="5432"
readonly PG_ROLE_NAME="yugabyte"
readonly PG_ROLE_PASSWORD="yugabyte"
readonly PG_DEFAULT_DB="yugabyte"
readonly PG_TEMPLATE_DB="itest_template"
readonly PG_TEMPLATE_SMOKE_DB="itest_template_smoke"
readonly PG_APT_KEYRING_DIR="/usr/share/postgresql-common/pgdg"
readonly PG_APT_KEYRING="${PG_APT_KEYRING_DIR}/apt.postgresql.org.asc"
readonly PG_APT_LIST="/etc/apt/sources.list.d/pgdg.list"
readonly PG_CONF_DIR="/etc/postgresql/${PG_VERSION}/${PG_CLUSTER}"
readonly PG_HBA_CONF="${PG_CONF_DIR}/pg_hba.conf"
readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
readonly HARMONY_SQL_DIR="${REPO_ROOT}/harmony/harmonydb/sql"

log() {
  echo "[INFO] $*"
}

warn() {
  echo "[WARN] $*" >&2
}

die() {
  echo "[ERROR] $*" >&2
  exit 1
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    die "This script must run as root."
  fi
}

apt_install() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y
  apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    tar \
    gzip \
    chrony \
    jq \
    netcat-openbsd \
    procps \
    lsb-release \
    locales
}

install_postgresql_apt_repo() {
  local version_codename

  log "Configuring PostgreSQL Apt repository"
  install -d "${PG_APT_KEYRING_DIR}"
  curl -o "${PG_APT_KEYRING}" --fail https://www.postgresql.org/media/keys/ACCC4CF8.asc

  version_codename="$(
    . /etc/os-release
    printf '%s' "${VERSION_CODENAME}"
  )"
  printf 'deb [signed-by=%s] https://apt.postgresql.org/pub/repos/apt %s-pgdg main\n' \
    "${PG_APT_KEYRING}" \
    "${version_codename}" > "${PG_APT_LIST}"

  apt-get update -y
}

install_postgresql() {
  log "Installing PostgreSQL ${PG_VERSION}"
  apt-get install -y --no-install-recommends \
    "postgresql-${PG_VERSION}" \
    "postgresql-client-${PG_VERSION}"
}

setup_locale() {
  log "Setup locale"
  locale-gen en_US.UTF-8
  update-locale LANG=en_US.UTF-8
}

setup_time_sync() {
  log "Configuring chrony"
  systemctl enable chrony
  systemctl restart chrony
  systemctl --no-pager --full status chrony >/dev/null 2>&1 || die "chrony failed to start"
}

create_yugabyte_user() {
  log "Creating yugabyte group/user"
  if ! getent group "${YB_GROUP}" >/dev/null; then
    groupadd --system "${YB_GROUP}"
  fi

  if ! id -u "${YB_USER}" >/dev/null 2>&1; then
    useradd \
      --system \
      --gid "${YB_GROUP}" \
      --home-dir "${YB_HOME}" \
      --create-home \
      --shell /usr/sbin/nologin \
      "${YB_USER}"
  fi
}

configure_dirs() {
  log "Creating install and data directories"
  install -d -o "${YB_USER}" -g "${YB_GROUP}" -m 0755 "${YB_INSTALL_ROOT}"
  install -d -o "${YB_USER}" -g "${YB_GROUP}" -m 0755 "${YB_BASE_DIR}"
}

install_yugabytedb() {
  local tmpdir tarball topdir unpacked_dir

  log "Downloading YugabyteDB ${YB_VERSION}"
  tmpdir="$(mktemp -d)"
  tarball="${tmpdir}/${YB_TARBALL}"

  # Important:
  # Expand tmpdir at trap definition time, not at trap execution time.
  trap 'rm -rf "'${tmpdir}'"' EXIT

  curl -fL --retry 5 --retry-delay 2 -o "${tarball}" "${YB_DOWNLOAD_URL}"

  log "Inspecting YugabyteDB archive layout"
  topdir="$(
    tar -tzf "${tarball}" \
      | awk -F/ 'NF && $1 != "" && !seen {
          name=$1
          sub(/^\.\//, "", name)
          if (name != "") {
            print name
            seen=1
          }
        }
        END {
          if (!seen) exit 1
        }'
  )"
  [[ -n "${topdir}" ]] || die "Could not determine top-level directory from ${tarball}"

  log "Extracting YugabyteDB"
  tar -xzf "${tarball}" -C "${YB_INSTALL_ROOT}"

  unpacked_dir="${YB_INSTALL_ROOT}/${topdir}"
  [[ -d "${unpacked_dir}" ]] || die "Expected extracted directory ${unpacked_dir} not found after extraction"

  ln -sfn "${unpacked_dir}" "${YB_CURRENT_LINK}"

  chown -R "${YB_USER}":"${YB_GROUP}" "${unpacked_dir}"
  chown -R "${YB_USER}":"${YB_GROUP}" "${YB_CURRENT_LINK}"

  rm -rf "${tmpdir}"
  trap - EXIT
}

configure_limits() {
  log "Configuring persistent limits in ${LIMITS_CONF}"
  cat > "${LIMITS_CONF}" <<'EOF'
yugabyte soft nofile 1048576
yugabyte hard nofile 1048576
yugabyte soft nproc 12000
yugabyte hard nproc 12000
yugabyte soft stack 8388608
yugabyte hard stack 8388608
yugabyte soft core unlimited
yugabyte hard core unlimited
yugabyte soft fsize unlimited
yugabyte hard fsize unlimited
yugabyte soft data unlimited
yugabyte hard data unlimited
yugabyte soft rss unlimited
yugabyte hard rss unlimited
yugabyte soft as unlimited
yugabyte hard as unlimited
yugabyte soft memlock 64
yugabyte hard memlock 64
yugabyte soft locks unlimited
yugabyte hard locks unlimited
yugabyte soft sigpending 119934
yugabyte hard sigpending 119934
yugabyte soft msgqueue 819200
yugabyte hard msgqueue 819200
EOF
  chmod 0644 "${LIMITS_CONF}"
}

configure_sysctl() {
  log "Configuring kernel settings in ${SYSCTL_CONF}"
  cat > "${SYSCTL_CONF}" <<'EOF'
vm.swappiness=0
vm.max_map_count=262144
EOF
  chmod 0644 "${SYSCTL_CONF}"

  sysctl --system >/dev/null
  [[ "$(sysctl -n vm.swappiness)" == "0" ]] || die "vm.swappiness was not applied"
  [[ "$(sysctl -n vm.max_map_count)" == "262144" ]] || die "vm.max_map_count was not applied"
}

configure_thp_on_next_boot() {
  log "Staging transparent hugepage bootloader config"
  if [[ ! -f /etc/default/grub ]]; then
    die "/etc/default/grub not found"
  fi

  cp -a /etc/default/grub /etc/default/grub.bak.yugabytedb

  python3 - <<'PY'
from pathlib import Path
path = Path("/etc/default/grub")
text = path.read_text()

needle = "transparent_hugepage=always"

lines = text.splitlines()
changed = False
for i, line in enumerate(lines):
    if line.startswith("GRUB_CMDLINE_LINUX="):
        if needle not in line:
            first = line.find('"')
            last = line.rfind('"')
            if first == -1 or last <= first:
                raise SystemExit("Could not parse GRUB_CMDLINE_LINUX")
            current = line[first+1:last].strip()
            newval = (current + " " + needle).strip()
            lines[i] = f'GRUB_CMDLINE_LINUX="{newval}"'
            changed = True
        break
else:
    lines.append(f'GRUB_CMDLINE_LINUX="{needle}"')
    changed = True

if changed:
    path.write_text("\n".join(lines) + "\n")
PY

  if [[ -d /sys/firmware/efi ]]; then
    update-grub
  else
    update-grub
  fi

  warn "THP bootloader setting has been staged. It will not be effective until the instance reboots."
}

write_systemd_unit() {
  log "Writing ${SYSTEMD_UNIT_PATH}"
  cat > "${SYSTEMD_UNIT_PATH}" <<EOF
[Unit]
Description=YugabyteDB managed by yugabyted

[Service]
Type=simple
User=yugabyte
Group=yugabyte
ExecStart=/opt/yugabyte/current/bin/yugabyted start --base_dir /var/lib/yugabytedb --listen 127.0.0.1 --callhome false --background=false
ExecStop=/opt/yugabyte/current/bin/yugabyted stop --base_dir /var/lib/yugabytedb
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576
TimeoutStopSec=120

[Install]
WantedBy=multi-user.target
EOF

  chmod 0644 "${SYSTEMD_UNIT_PATH}"
  systemctl daemon-reload
  systemctl enable "${YB_SERVICE_NAME}"
}

start_service() {
  log "Starting ${YB_SERVICE_NAME}"
  systemctl restart "${YB_SERVICE_NAME}"
}

wait_for_tcp() {
  local host="$1"
  local port="$2"
  local attempts="${3:-60}"
  local sleep_s="${4:-2}"

  for ((i=1; i<=attempts; i++)); do
    if nc -z "${host}" "${port}" >/dev/null 2>&1; then
      return 0
    fi
    sleep "${sleep_s}"
  done

  return 1
}

wait_for_http_200() {
  local url="$1"
  local attempts="${2:-60}"
  local sleep_s="${3:-2}"
  local code

  for ((i=1; i<=attempts; i++)); do
    code="$(curl -s -o /dev/null -w '%{http_code}' "${url}" || true)"
    if [[ "${code}" == "200" ]]; then
      return 0
    fi
    sleep "${sleep_s}"
  done

  return 1
}

validate_service() {
  log "Validating systemd state"
  [[ "$(systemctl is-active "${YB_SERVICE_NAME}")" == "active" ]] || {
    systemctl --no-pager --full status "${YB_SERVICE_NAME}" || true
    die "${YB_SERVICE_NAME} is not active"
  }
}

validate_ysql_tcp() {
  log "Waiting for TCP ${YB_LISTEN_IP}:${YB_YSQL_PORT}"
  wait_for_tcp "${YB_LISTEN_IP}" "${YB_YSQL_PORT}" 90 2 || die "YSQL TCP port ${YB_YSQL_PORT} is not reachable"
}

validate_ui() {
  log "Waiting for HTTP 200 on http://${YB_LISTEN_IP}:${YB_UI_PORT}"
  wait_for_http_200 "http://${YB_LISTEN_IP}:${YB_UI_PORT}" 90 2 || die "UI did not return HTTP 200"
}

validate_sql() {
  log "Running SQL validation via ysqlsh"
  sudo -u "${YB_USER}" -H "${YB_YSQLSH}" \
    -h "${YB_LISTEN_IP}" \
    -p "${YB_YSQL_PORT}" \
    -U yugabyte \
    -d yugabyte \
    -Atqc "SELECT 1;" | grep -qx '1' || die "SQL validation failed"
}

configure_postgresql() {
  log "Configuring PostgreSQL ${PG_VERSION}"
  [[ -d "${PG_CONF_DIR}" ]] || die "PostgreSQL config directory ${PG_CONF_DIR} not found"
  [[ -f "${PG_HBA_CONF}" ]] || die "PostgreSQL pg_hba.conf ${PG_HBA_CONF} not found"

  pg_conftool "${PG_VERSION}" "${PG_CLUSTER}" set listen_addresses "'${PG_LISTEN_IP}'"
  pg_conftool "${PG_VERSION}" "${PG_CLUSTER}" set port "${PG_PORT}"

  python3 - <<PY
from pathlib import Path
path = Path("${PG_HBA_CONF}")
text = path.read_text()
managed = [
    "host all all 127.0.0.1/32 scram-sha-256",
    "host all all ::1/128 scram-sha-256",
]
lines = [line for line in text.splitlines() if line.strip() not in managed]
path.write_text("\\n".join(managed + lines) + "\\n")
PY

  systemctl daemon-reload
  systemctl enable "${PG_CLUSTER_UNIT}"
  systemctl restart "${PG_CLUSTER_UNIT}"
}

validate_postgresql_service() {
  log "Validating PostgreSQL cluster systemd state"
  [[ "$(systemctl is-active "${PG_CLUSTER_UNIT}")" == "active" ]] || {
    systemctl --no-pager --full status "${PG_CLUSTER_UNIT}" || true
    die "${PG_CLUSTER_UNIT} is not active"
  }
}

validate_postgresql_tcp() {
  log "Waiting for TCP ${PG_LISTEN_IP}:${PG_PORT}"
  wait_for_tcp "${PG_LISTEN_IP}" "${PG_PORT}" 90 2 || die "PostgreSQL TCP port ${PG_PORT} is not reachable"
}

postgres_superuser_sql() {
  local database="$1"
  local sql="$2"
  sudo -u postgres -H psql \
    -X \
    -v ON_ERROR_STOP=1 \
    -d "${database}" \
    -Atqc "${sql}"
}

postgres_yugabyte_sql() {
  local database="$1"
  local sql="$2"
  PGPASSWORD="${PG_ROLE_PASSWORD}" psql \
    -X \
    -v ON_ERROR_STOP=1 \
    -h "${PG_LISTEN_IP}" \
    -p "${PG_PORT}" \
    -U "${PG_ROLE_NAME}" \
    -d "${database}" \
    -Atqc "${sql}"
}

postgres_yugabyte_file() {
  local database="$1"
  local file="$2"
  PGPASSWORD="${PG_ROLE_PASSWORD}" psql \
    -X \
    -v ON_ERROR_STOP=1 \
    -h "${PG_LISTEN_IP}" \
    -p "${PG_PORT}" \
    -U "${PG_ROLE_NAME}" \
    -d "${database}" \
    -f "${file}"
}

bootstrap_postgresql_cluster() {
  log "Bootstrapping PostgreSQL roles and databases"

  postgres_superuser_sql postgres "CREATE ROLE ${PG_ROLE_NAME} WITH LOGIN PASSWORD '${PG_ROLE_PASSWORD}' SUPERUSER CREATEDB CREATEROLE;"
  postgres_superuser_sql postgres "CREATE DATABASE ${PG_DEFAULT_DB} WITH OWNER = ${PG_ROLE_NAME} TEMPLATE = template0;"
  postgres_superuser_sql postgres "CREATE DATABASE ${PG_TEMPLATE_DB} WITH OWNER = ${PG_ROLE_NAME} TEMPLATE = template0 IS_TEMPLATE = true;"
}

load_harmony_schema_into_template() {
  local file name entry

  log "Loading Harmony schema into PostgreSQL template database"
  [[ -d "${HARMONY_SQL_DIR}" ]] || die "Harmony SQL directory ${HARMONY_SQL_DIR} not found"

  postgres_yugabyte_sql "${PG_TEMPLATE_DB}" "CREATE TABLE IF NOT EXISTS base (
    id SERIAL PRIMARY KEY,
    entry CHAR(12),
    applied TIMESTAMP DEFAULT current_timestamp
  );"

  shopt -s nullglob
  for file in "${HARMONY_SQL_DIR}"/*.sql; do
    name="$(basename "${file}")"
    entry="${name:0:8}"

    log "Applying PostgreSQL template migration ${name}"
    postgres_yugabyte_file "${PG_TEMPLATE_DB}" "${file}"
    postgres_yugabyte_sql "${PG_TEMPLATE_DB}" "INSERT INTO base (entry) VALUES ('${entry}');"
  done
  shopt -u nullglob
}

validate_postgresql_sql() {
  log "Running PostgreSQL SQL validation"
  postgres_yugabyte_sql "${PG_DEFAULT_DB}" "SELECT 1;" | grep -qx '1' || die "PostgreSQL SQL validation failed"
}

validate_postgresql_template() {
  local expected actual

  log "Validating PostgreSQL template database"
  postgres_yugabyte_sql "${PG_TEMPLATE_DB}" "SELECT 1;" | grep -qx '1' || die "Template database SQL validation failed"

  expected="$(find "${HARMONY_SQL_DIR}" -maxdepth 1 -type f -name '*.sql' | wc -l | tr -d ' ')"
  actual="$(postgres_yugabyte_sql "${PG_TEMPLATE_DB}" "SELECT COUNT(*) FROM base;")"
  [[ "${actual}" == "${expected}" ]] || die "Template migration count mismatch: expected ${expected}, got ${actual}"

  postgres_superuser_sql postgres "CREATE DATABASE ${PG_TEMPLATE_SMOKE_DB} WITH OWNER = ${PG_ROLE_NAME} TEMPLATE = ${PG_TEMPLATE_DB};"
  postgres_yugabyte_sql "${PG_TEMPLATE_SMOKE_DB}" "SELECT to_regclass('base') IS NOT NULL AND to_regclass('harmony_task') IS NOT NULL;" | grep -qx 't' || die "Template clone validation failed"
  postgres_superuser_sql postgres "DROP DATABASE ${PG_TEMPLATE_SMOKE_DB};"
}

# -----------------------------------------------------------------------------
# Install Lotus and prefetch Filecoin proof parameters
# -----------------------------------------------------------------------------

readonly LOTUS_URL="https://github.com/filecoin-project/lotus/releases/download/v1.35.1/lotus_v1.35.1_linux_amd64_v1.tar.gz"
readonly LOTUS_SHA256="53bf47ba63b4ddc6afe12e9eda28e9dfd76a7e4a3174f3bd44523110f7f61802"
readonly LOTUS_TMP_DIR="/tmp/lotus-install"

install_lotus_and_fetch_params() {
  local tarball
  local lotus_bin

  log "Downloading Lotus"
  rm -rf "${LOTUS_TMP_DIR}"
  mkdir -p "${LOTUS_TMP_DIR}"

  tarball="${LOTUS_TMP_DIR}/lotus.tar.gz"
  curl -fL --retry 5 --retry-delay 2 -o "${tarball}" "${LOTUS_URL}"

  log "Verifying Lotus checksum"
  echo "${LOTUS_SHA256}  ${tarball}" | sha256sum -c -

  log "Extracting Lotus"
  tar -xzf "${tarball}" -C "${LOTUS_TMP_DIR}"

  lotus_bin="$(find "${LOTUS_TMP_DIR}" -type f -name lotus | head -n 1)"
  [[ -n "${lotus_bin}" ]] || die "lotus binary not found in extracted archive"

  install -o root -g root -m 0755 "${lotus_bin}" /usr/local/bin/lotus

  log "Install lotus dependencies"
  apt-get install -y mesa-opencl-icd ocl-icd-opencl-dev gcc git jq pkg-config curl clang build-essential hwloc libhwloc-dev wget

  log "Fetching Filecoin proof parameters"
  /usr/local/bin/lotus fetch-params "2KiB"
  /usr/local/bin/lotus fetch-params "8MiB"

  rm -rf "${LOTUS_TMP_DIR}"
}

main() {
  require_root

  log "Installing OS dependencies"
  apt_install

  # Setup Locale for YB
  setup_locale

  # YB setup
  setup_time_sync
  create_yugabyte_user
  configure_dirs
  install_yugabytedb
  configure_limits
  configure_sysctl
  configure_thp_on_next_boot
  write_systemd_unit
  start_service
  validate_service
  validate_ysql_tcp
  validate_ui
  validate_sql

  # Postgres setup
  install_postgresql_apt_repo
  install_postgresql
  configure_postgresql
  validate_postgresql_service
  validate_postgresql_tcp
  bootstrap_postgresql_cluster
  validate_postgresql_sql
  load_harmony_schema_into_template
  validate_postgresql_template

  # Params download
  install_lotus_and_fetch_params

  log "Provisioning completed successfully"
  warn "Transparent hugepages bootloader change is staged but not effective until first reboot."
}

main "$@"
