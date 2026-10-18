#!/usr/bin/env bash
# Create 10 LVM volumes, about 8.1–12.6 GiB each, and mount them under the
# directory Docker Skiff bind-mounts at /data (SKIFF_STORAGE, default ./data).
# Free space across the ten filesystems is just over 100 GiB.
#
# The admin GUI lists those folders as storage candidates. Attach them from
# http://127.0.0.1:4701 → Storage. Paths inside the container are
# /data/lvm-test-01 .. /data/lvm-test-10.
#
# The running skiff container does not see mounts created after it started.
# `up` restarts the skiff container when it is already running. Yugabyte and
# Forest are left alone. Pass --no-restart to skip that.
#
#   sudo scripts/manual_test/lvm-test-volumes.sh up
#   sudo scripts/manual_test/lvm-test-volumes.sh status
#   sudo scripts/manual_test/lvm-test-volumes.sh down
#
# Volumes do not survive a reboot. Run `up` again to reattach the same image.

set -euo pipefail

COUNT=10
# Linear sizes. The sum of ext4 free space is just over 100 GiB.
MIN_MIB=8256
MAX_MIB=12936
VG_NAME="${VG_NAME:-skifflvmtest}"
BACKING_DIR="${BACKING_DIR:-/var/lib/skiff-lvm-test}"
BACKING_FILE="${BACKING_DIR}/backing.img"
# Arithmetic sum of the linear sizes, plus room for LVM metadata.
BACKING_MIB=$(( (MIN_MIB + MAX_MIB) * COUNT / 2 + 512 ))
MOUNT_PREFIX="lvm-test-"

RESTART=1
COMMAND=""

usage() {
  cat <<'EOF'
Usage: lvm-test-volumes.sh [--no-restart] up|status|down

  up       Allocate a loop-backed volume group, create 10 logical volumes
           totaling just over 100 GiB of free space, and mount them under
           the Skiff data directory.
  status   Show volume sizes and mount points.
  down     Unmount, remove the volume group, and delete the backing image.

  --no-restart   Do not restart the skiff container after mounting.

Environment:
  SKIFF_STORAGE   Host directory mounted at /data (default: docker/skiff/.env,
                  then docker/skiff/data).
  VG_NAME         Volume group name (default: skifflvmtest).
  BACKING_DIR     Loop image directory (default: /var/lib/skiff-lvm-test).
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    up|status|down)
      COMMAND="$1"
      ;;
    --no-restart)
      RESTART=0
      ;;
    --restart)
      RESTART=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ -z "$COMMAND" ]]; then
  usage >&2
  exit 2
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "run as root (sudo $0 $COMMAND)" >&2
  exit 1
fi

for cmd in fallocate losetup pvcreate vgcreate lvcreate vgchange vgs lvs pvs mkfs.ext4 mountpoint findmnt docker; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "required command not found: $cmd" >&2
    exit 1
  fi
done

# LVM 2.03 hides loop devices when a system devices file is enabled.
LVM=(--devicesfile "")

size_mib() {
  local i="$1"
  echo $(( MIN_MIB + (MAX_MIB - MIN_MIB) * i / (COUNT - 1) ))
}

vol_name() {
  printf 'vol%02d' "$1"
}

mount_name() {
  printf '%s%02d' "$MOUNT_PREFIX" "$1"
}

resolve_storage_root() {
  local script_dir repo_root compose_dir val
  script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
  repo_root=$(cd "${script_dir}/../.." && pwd)
  compose_dir="${repo_root}/docker/skiff"
  if [[ -n "${SKIFF_STORAGE:-}" ]]; then
    val="$SKIFF_STORAGE"
  elif [[ -f "${compose_dir}/.env" ]]; then
    val=$(grep -E '^[[:space:]]*SKIFF_STORAGE=' "${compose_dir}/.env" | tail -1 | cut -d= -f2- | tr -d '"' | tr -d "'" || true)
    val="${val#"${val%%[![:space:]]*}"}"
    val="${val%"${val##*[![:space:]]}"}"
  fi
  val="${val:-./data}"
  if [[ "$val" != /* ]]; then
    val="${compose_dir}/${val}"
  fi
  if [[ ! -d "$val" ]]; then
    echo "Skiff storage directory does not exist: $val" >&2
    exit 1
  fi
  (cd "$val" && pwd)
}

STORAGE_ROOT=$(resolve_storage_root)

lv_device() {
  echo "/dev/${VG_NAME}/$(vol_name "$1")"
}

mount_point() {
  echo "${STORAGE_ROOT}/$(mount_name "$1")"
}

vg_exists() {
  vgs "${LVM[@]}" "$VG_NAME" >/dev/null 2>&1
}

loop_for_backing() {
  local out
  out=$(losetup -j "$BACKING_FILE" || true)
  if [[ -z "$out" ]]; then
    return 0
  fi
  echo "${out%%:*}"
}

ensure_loop() {
  local loop
  loop=$(loop_for_backing)
  if [[ -z "$loop" ]]; then
    loop=$(losetup --find --show "$BACKING_FILE")
  fi
  echo "$loop"
}

activate_vg() {
  vgchange "${LVM[@]}" -ay "$VG_NAME" >/dev/null
}

mount_one() {
  local i="$1"
  local n dest dev
  n=$((i + 1))
  dest=$(mount_point "$n")
  dev=$(lv_device "$n")
  mkdir -p "$dest"
  if ! mountpoint -q "$dest"; then
    mount "$dev" "$dest"
  fi
  # ext4 lost+found is writable by root, so Skiff would offer it as its own candidate.
  if [[ -d "${dest}/lost+found" ]]; then
    rmdir "${dest}/lost+found" 2>/dev/null || true
  fi
  chmod 755 "$dest"
}

print_table() {
  local i n dest dev mib src
  printf '%-28s %8s  %s\n' "CONTAINER PATH" "SIZE" "HOST MOUNT"
  for i in $(seq 0 $((COUNT - 1))); do
    n=$((i + 1))
    dest=$(mount_point "$n")
    dev=$(lv_device "$n")
    mib=$(size_mib "$i")
    src=$(findmnt -n -o SOURCE --target "$dest" 2>/dev/null || true)
    # findmnt prints the mapper node; lvcreate's path is a symlink to it.
    if [[ -n "$src" && "$(readlink -f "$src")" == "$(readlink -f "$dev")" ]]; then
      printf '%-28s %6sM  %s\n' "/data/$(mount_name "$n")" "$mib" "$dest"
    else
      printf '%-28s %6sM  %s (not mounted)\n' "/data/$(mount_name "$n")" "$mib" "$dest"
    fi
  done
}

skiff_running() {
  docker ps --format '{{.Names}}' | grep -qx 'skiff'
}

restart_skiff_if_needed() {
  if [[ "$RESTART" -ne 1 ]]; then
    echo "Skipped skiff restart (--no-restart)."
    echo "Restart it before attaching paths: docker restart skiff"
    return 0
  fi
  if skiff_running; then
    echo "Restarting skiff so the new mounts show up under /data ..."
    docker restart skiff >/dev/null
    echo "skiff restarted."
  else
    echo "skiff is not running. Start the stack, then attach the folders in the GUI."
  fi
}

cmd_up() {
  local i n loop actual_mib
  mkdir -p "$BACKING_DIR"

  if [[ -f "$BACKING_FILE" ]]; then
    actual_mib=$(( $(stat -c%s "$BACKING_FILE") / 1048576 ))
    if (( actual_mib + 1 < BACKING_MIB )) && ! vg_exists; then
      echo "backing file is ${actual_mib}MiB but ${BACKING_MIB}MiB is required; run down first" >&2
      exit 1
    fi
  else
    echo "Allocating ${BACKING_MIB}MiB backing image at ${BACKING_FILE}"
    fallocate -l "$((BACKING_MIB * 1024 * 1024))" "$BACKING_FILE"
  fi

  loop=$(ensure_loop)
  echo "Loop device: ${loop}"

  if ! vg_exists; then
    if ! pvs "${LVM[@]}" "$loop" >/dev/null 2>&1; then
      pvcreate "${LVM[@]}" "$loop"
    fi
    vgcreate "${LVM[@]}" "$VG_NAME" "$loop"
  fi
  activate_vg

  for i in $(seq 0 $((COUNT - 1))); do
    n=$((i + 1))
    if ! lvs "${LVM[@]}" "${VG_NAME}/$(vol_name "$n")" >/dev/null 2>&1; then
      lvcreate "${LVM[@]}" -y -L "$(size_mib "$i")M" -n "$(vol_name "$n")" "$VG_NAME"
      mkfs.ext4 -F -m 0 -L "skiff-$(vol_name "$n")" "$(lv_device "$n")"
    fi
    mount_one "$i"
  done

  echo
  echo "Mounted under ${STORAGE_ROOT} (container /data):"
  print_table
  echo
  restart_skiff_if_needed
  echo "Attach /data/${MOUNT_PREFIX}01 .. /data/${MOUNT_PREFIX}$(printf '%02d' "$COUNT") in the admin GUI."
}

cmd_status() {
  if vg_exists; then
    activate_vg || true
    lvs "${LVM[@]}" "$VG_NAME"
    echo
  else
    echo "Volume group ${VG_NAME} is not present."
  fi
  print_table
  if skiff_running; then
    echo
    echo "Visible inside the skiff container:"
    docker exec skiff ls -d /data/${MOUNT_PREFIX}* 2>/dev/null || echo "(none)"
  fi
}

cmd_down() {
  local i n dest loop
  WAS_RUNNING=0
  if skiff_running; then
    WAS_RUNNING=1
    echo "Stopping skiff so the mounts can be removed ..."
    docker stop skiff >/dev/null
  fi
  # Shell EXIT, not function return: set -e leaves this trap in place if a later step fails.
  trap 'if [[ "${WAS_RUNNING:-0}" -eq 1 ]]; then docker start skiff >/dev/null || true; fi' EXIT

  for i in $(seq 0 $((COUNT - 1))); do
    n=$((i + 1))
    dest=$(mount_point "$n")
    if mountpoint -q "$dest"; then
      umount "$dest"
    fi
    if [[ -d "$dest" ]]; then
      rmdir "$dest" 2>/dev/null || echo "left non-empty directory: ${dest}" >&2
    fi
  done

  if vg_exists; then
    lvchange "${LVM[@]}" -an "$VG_NAME" || true
    lvremove "${LVM[@]}" -y "$VG_NAME" || true
    vgremove "${LVM[@]}" -y "$VG_NAME"
  fi

  loop=$(loop_for_backing || true)
  if [[ -n "$loop" ]]; then
    if vg_exists; then
      pvremove "${LVM[@]}" -y "$loop" || true
    else
      pvremove "${LVM[@]}" -y "$loop" 2>/dev/null || true
    fi
    losetup -d "$loop"
  fi

  rm -f "$BACKING_FILE"
  rmdir "$BACKING_DIR" 2>/dev/null || true

  trap - EXIT
  if [[ "$WAS_RUNNING" -eq 1 ]]; then
    echo "Starting skiff ..."
    docker start skiff >/dev/null
  fi
  echo "Removed ${VG_NAME}."
}

case "$COMMAND" in
  up) cmd_up ;;
  status) cmd_status ;;
  down) cmd_down ;;
esac
