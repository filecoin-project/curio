#!/usr/bin/env bash
set -e

# Piece-server devnet entrypoint.
#
# Startup phases:
# 1. Wait for Lotus API and head threshold so miner bootstrap side effects settle.
# 2. Start contract bootstrap worker in background.
# 3. Start synapse-sdk bootstrap worker in background.
# 4. Run mk12/FIL+/datacap setup in foreground.
# 5. Start piece-server HTTP process after mk12 succeeds.
# 6. Keep both background workers running in parallel until they converge:
#    - ensure a delegated test user exists and is funded
#    - configure user allowances/deposits for FilecoinPay/FWSS
#    - emit foc-devnet-compatible devnet-info.json
#    - clone/build synapse-sdk for manual e2e usage
# 7. Fail fast if either bootstrap worker does not converge.

# Shared paths and runtime settings.
CURIO_MK12_CLIENT_REPO=$CURIO_MK12_CLIENT_REPO
CONTRACT_ENV_FILE="${CONTRACT_ENV_FILE:-/var/lib/contracts/curio-devnet.env}"
CONTRACT_ADDRESSES_FILE="${CONTRACT_ADDRESSES_FILE:-/var/lib/contracts/contract_addresses.json}"
DEVNET_INFO_FILE="${DEVNET_INFO_FILE:-/var/lib/contracts/devnet-info.json}"
DEVNET_INFO_COMPAT_FILE="${DEVNET_INFO_COMPAT_FILE:-/root/.foc-devnet/state/latest/devnet-info.json}"
LOTUS_RPC_URL="${LOTUS_RPC_URL:-http://lotus:1234/rpc/v1}"

# Synapse user material persisted in piece-server writable volume.
SYNAPSE_USER_NAME="${SYNAPSE_USER_NAME:-USER_1}"
SYNAPSE_USER_FUND_AMOUNT="${SYNAPSE_USER_FUND_AMOUNT:-1000}"
SYNAPSE_USER_DIR="${CURIO_MK12_CLIENT_REPO}/synapse"
SYNAPSE_USER_ADDR_FILE="${SYNAPSE_USER_DIR}/user_1.t4"
SYNAPSE_USER_PRIV_FILE="${SYNAPSE_USER_DIR}/user_1.private-key"
SYNAPSE_USER_EVM_FILE="${SYNAPSE_USER_DIR}/user_1.evm"

# Contract/bootstrap markers.
DEPLOYER_PRIVATE_KEY_FILE="${DEPLOYER_PRIVATE_KEY_FILE:-/var/lib/contracts/deployer.private-key}"
CLIENT_READY_FILE="${CLIENT_READY_FILE:-/var/lib/contracts/client.ready}"
CLIENT_LOCK_FILE="${CLIENT_LOCK_FILE:-/var/lib/contracts/client.bootstrap.lock}"
DEPLOYER_TX_LOCK_FILE="${DEPLOYER_TX_LOCK_FILE:-/var/lib/contracts/deployer.tx.lock}"
USER1_USDFC_FUND_AMOUNT_WEI="${USER1_USDFC_FUND_AMOUNT_WEI:-100000000000000000000000}"
USER1_DEPOSIT_AMOUNT_WEI="${USER1_DEPOSIT_AMOUNT_WEI:-1000000000000000000}"
USER1_LOCKUP_EPOCHS="${USER1_LOCKUP_EPOCHS:-86400}"
CLIENT_BOOTSTRAP_RETRY_INTERVAL_SEC="${CLIENT_BOOTSTRAP_RETRY_INTERVAL_SEC:-5}"
CLIENT_BOOTSTRAP_MAX_RETRIES="${CLIENT_BOOTSTRAP_MAX_RETRIES:-120}"

# Synapse SDK bootstrap workspace for manual e2e scripts.
SYNAPSE_SDK_REPO_URL="https://github.com/FilOzone/synapse-sdk.git"
SYNAPSE_SDK_REPO_DIR="/var/lib/curio-client/synapse-sdk"
SYNAPSE_SDK_READY_FILE="/var/lib/curio-client/.synapse-sdk.ready"
SYNAPSE_SDK_LOCK_FILE="/var/lib/curio-client/.synapse-sdk.bootstrap.lock"

# Wait until a delegated actor is present on-chain.
wait_for_actor() {
  local addr="$1"
  local tries=0
  until lotus state get-actor "$addr" >/dev/null 2>&1; do
    tries=$((tries + 1))
    if [[ "$tries" -gt 180 ]]; then
      echo "Actor not found after 180s: $addr"
      return 1
    fi
    sleep 1
  done
}

# Wait for all contract artifacts produced by contracts-bootstrap.
wait_for_contract_artifacts() {
  local tries=0
  while [[ ! -f "$CONTRACT_ENV_FILE" || ! -f "$CONTRACT_ADDRESSES_FILE" || ! -f "$DEPLOYER_PRIVATE_KEY_FILE" ]]; do
    tries=$((tries + 1))
    if [[ "$tries" -gt 300 ]]; then
      echo "Contract artifacts not found in time: $CONTRACT_ENV_FILE / $CONTRACT_ADDRESSES_FILE / $DEPLOYER_PRIVATE_KEY_FILE"
      return 1
    fi
    sleep 1
  done
}

# Validate canonical 20-byte EVM address format.
require_hex_address() {
  local label="$1"
  local value="$2"
  [[ "$value" =~ ^0x[0-9a-fA-F]{40}$ ]] || {
    echo "Missing or invalid ${label}: ${value:-<empty>}"
    return 1
  }
}

# Normalize cast output to decimal string.
to_decimal() {
  local raw
  raw="$(echo "${1:-0}" | tr -d '\r\n' | awk '{print $1}')"
  if [[ -z "$raw" ]]; then
    echo "0"
    return 0
  fi
  if [[ "$raw" =~ ^0x[0-9a-fA-F]+$ ]]; then
    cast to-dec "$raw" | tr -d '\r\n[:space:]'
    return $?
  fi
  if [[ "$raw" =~ ^[0-9]+$ ]]; then
    echo "$raw"
    return 0
  fi
  return 1
}

# Serialize deployer-key transactions across containers to avoid nonce races.
with_deployer_tx_lock() {
  local lock_dir
  lock_dir="$(dirname "$DEPLOYER_TX_LOCK_FILE")"
  mkdir -p "$lock_dir" 2>/dev/null || {
    echo "[client-bootstrap][error] unable to create deployer tx lock dir: $lock_dir" >&2
    return 1
  }
  touch "$DEPLOYER_TX_LOCK_FILE" 2>/dev/null || {
    echo "[client-bootstrap][error] unable to write deployer tx lock file: $DEPLOYER_TX_LOCK_FILE" >&2
    return 1
  }
  if command -v flock >/dev/null 2>&1; then
    (
      flock 8
      "$@"
    ) 8>"$DEPLOYER_TX_LOCK_FILE"
  else
    "$@"
  fi
}

client_log() {
  echo "[client-bootstrap] $*"
}

client_fail() {
  echo "[client-bootstrap][error] $*" >&2
  return 1
}

synapse_log() {
  echo "[synapse-bootstrap] $*"
}

synapse_fail() {
  echo "[synapse-bootstrap][error] $*" >&2
  return 1
}

# Ensure USER_1 exists and has:
# - delegated t4 address
# - corresponding private key
# - corresponding EVM address
# - FIL funding so actor exists on-chain
ensure_synapse_user() {
  client_log "Ensuring delegated test user exists"
  mkdir -p "$SYNAPSE_USER_DIR"

  local user_native_addr user_private_key_hex user_evm_addr default_wallet fund_cid

  # Reuse persisted user address if present, otherwise create one.
  if [[ -f "$SYNAPSE_USER_ADDR_FILE" ]]; then
    user_native_addr="$(tr -d '\r\n' < "$SYNAPSE_USER_ADDR_FILE")"
  else
    user_native_addr="$(lotus wallet new delegated | tr -d '\r\n')"
    echo "$user_native_addr" > "$SYNAPSE_USER_ADDR_FILE"
    client_log "Created delegated user address: $user_native_addr"
  fi

  # Reuse persisted private key if present, otherwise export from Lotus wallet.
  if [[ -f "$SYNAPSE_USER_PRIV_FILE" ]]; then
    user_private_key_hex="$(tr -d '\r\n' < "$SYNAPSE_USER_PRIV_FILE")"
    user_private_key_hex="${user_private_key_hex#0x}"
  else
    user_private_key_hex="$(lotus wallet export "$user_native_addr" | xxd -r -p | jq -r '.PrivateKey' | base64 -d | xxd -p -c 256 | tr -d '\r\n')"
    printf '0x%s\n' "$user_private_key_hex" > "$SYNAPSE_USER_PRIV_FILE"
  fi

  user_evm_addr="$(cast wallet address --private-key "0x${user_private_key_hex}" | tr -d '\r\n')"
  echo "$user_evm_addr" > "$SYNAPSE_USER_EVM_FILE"
  client_log "Resolved user EVM address: $user_evm_addr"

  # Ensure actor exists by funding once from default wallet.
  if ! lotus state get-actor "$user_native_addr" >/dev/null 2>&1; then
    client_log "User actor not on-chain yet; funding $user_native_addr with $SYNAPSE_USER_FUND_AMOUNT FIL"
    default_wallet="$(lotus wallet default | tr -d '\r\n')"
    fund_cid="$(lotus send --from "$default_wallet" "$user_native_addr" "$SYNAPSE_USER_FUND_AMOUNT" | awk '/^bafy/{print $1}' | tail -n1 | tr -d '\r\n')"
    [[ -n "$fund_cid" ]] || { client_fail "failed to submit lotus funding message for user actor creation"; return 1; }
    lotus state wait-msg "$fund_cid" >/dev/null
  fi
  wait_for_actor "$user_native_addr" || { client_fail "user actor did not appear on-chain in time: $user_native_addr"; return 1; }
  client_log "User actor ready: $user_native_addr"
}

# Export a foc-devnet-compatible devnet-info.json for synapse NETWORK=devnet mode.
# This includes user credentials, core contract addresses, and Curio provider metadata.
write_devnet_info() {
  client_log "Writing devnet-info.json"
  wait_for_contract_artifacts || { client_fail "contract artifacts are not ready"; return 1; }

  # shellcheck disable=SC1090
  source "$CONTRACT_ENV_FILE"

  local chain_id run_id start_time startup_duration
  local user_native_addr user_private_key user_evm_addr
  local pdp_verifier_proxy_addr pdp_verifier_impl_addr
  local fwss_proxy_addr fwss_impl_addr fwss_view_addr
  local service_registry_proxy_addr service_registry_impl_addr
  local filecoin_pay_addr usdfc_addr multicall3_addr session_key_registry_addr endorsements_addr
  local provider_native_addr provider_private_key_hex provider_eth_addr
  local provider_id_raw provider_id_dec provider_id
  local is_approved_raw is_endorsed_raw is_approved_json is_endorsed_json

  # Metadata fields expected by synapse examples.
  chain_id="$(jq -r '.chain_id // "31415926"' "$CONTRACT_ADDRESSES_FILE")"
  run_id="curio-devnet-$(date -u +'%Y%m%d%H%M%S')"
  start_time="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  startup_duration="0s"

  user_native_addr="$(tr -d '\r\n' < "$SYNAPSE_USER_ADDR_FILE")"
  user_private_key="$(tr -d '\r\n' < "$SYNAPSE_USER_PRIV_FILE")"
  user_evm_addr="$(tr -d '\r\n' < "$SYNAPSE_USER_EVM_FILE")"

  # Resolve all required contracts from JSON first, then env fallbacks.
  pdp_verifier_proxy_addr="$(jq -r '.contracts.pdp_verifier_proxy // .contracts.pdp_verifier // empty' "$CONTRACT_ADDRESSES_FILE")"
  pdp_verifier_impl_addr="$(jq -r '.contracts.pdp_verifier_implementation // empty' "$CONTRACT_ADDRESSES_FILE")"
  fwss_proxy_addr="$(jq -r '.contracts.filecoin_warm_storage_service_proxy // .contracts.filecoin_warm_storage_service // empty' "$CONTRACT_ADDRESSES_FILE")"
  fwss_impl_addr="$(jq -r '.contracts.filecoin_warm_storage_service_implementation // empty' "$CONTRACT_ADDRESSES_FILE")"
  fwss_view_addr="$(jq -r '.contracts.filecoin_warm_storage_service_state_view // empty' "$CONTRACT_ADDRESSES_FILE")"
  service_registry_proxy_addr="$(jq -r '.contracts.service_provider_registry_proxy // .contracts.service_provider_registry // empty' "$CONTRACT_ADDRESSES_FILE")"
  service_registry_impl_addr="$(jq -r '.contracts.service_provider_registry_implementation // empty' "$CONTRACT_ADDRESSES_FILE")"
  filecoin_pay_addr="$(jq -r '.contracts.filecoin_pay_v1 // empty' "$CONTRACT_ADDRESSES_FILE")"
  usdfc_addr="$(jq -r '.contracts.usdfc // empty' "$CONTRACT_ADDRESSES_FILE")"
  multicall3_addr="$(jq -r '.contracts.multicall3 // empty' "$CONTRACT_ADDRESSES_FILE")"
  session_key_registry_addr="$(jq -r '.contracts.session_key_registry // empty' "$CONTRACT_ADDRESSES_FILE")"
  endorsements_addr="$(jq -r '.contracts.endorsements // empty' "$CONTRACT_ADDRESSES_FILE")"

  [[ -n "$pdp_verifier_impl_addr" ]] || pdp_verifier_impl_addr="${CURIO_DEVNET_PDP_VERIFIER_IMPLEMENTATION_ADDRESS:-$pdp_verifier_proxy_addr}"
  [[ -n "$fwss_impl_addr" ]] || fwss_impl_addr="${CURIO_DEVNET_FWSS_IMPLEMENTATION_ADDRESS:-$fwss_proxy_addr}"
  [[ -n "$fwss_view_addr" ]] || fwss_view_addr="${CURIO_DEVNET_FWSS_STATE_VIEW_ADDRESS:-}"
  [[ -n "$service_registry_impl_addr" ]] || service_registry_impl_addr="${CURIO_DEVNET_SERVICE_REGISTRY_IMPLEMENTATION_ADDRESS:-$service_registry_proxy_addr}"
  [[ -n "$filecoin_pay_addr" ]] || filecoin_pay_addr="${CURIO_DEVNET_FILECOIN_PAY_ADDRESS:-}"
  [[ -n "$session_key_registry_addr" ]] || session_key_registry_addr="${CURIO_DEVNET_SESSION_KEY_REGISTRY_ADDRESS:-}"
  [[ -n "$endorsements_addr" ]] || endorsements_addr="${CURIO_DEVNET_ENDORSEMENTS_ADDRESS:-}"

  require_hex_address "PDP verifier proxy" "$pdp_verifier_proxy_addr"
  require_hex_address "PDP verifier implementation" "$pdp_verifier_impl_addr"
  require_hex_address "FWSS proxy" "$fwss_proxy_addr"
  require_hex_address "FWSS implementation" "$fwss_impl_addr"
  require_hex_address "FWSS view" "$fwss_view_addr"
  require_hex_address "Service registry proxy" "$service_registry_proxy_addr"
  require_hex_address "Service registry implementation" "$service_registry_impl_addr"
  require_hex_address "FilecoinPay" "$filecoin_pay_addr"
  require_hex_address "USDFC" "$usdfc_addr"
  require_hex_address "Multicall3" "$multicall3_addr"
  require_hex_address "SessionKeyRegistry" "$session_key_registry_addr"
  require_hex_address "Endorsements" "$endorsements_addr"

  # Provider metadata comes from Curio PDP wallet marker.
  [[ -f /var/lib/curio/pdp_wallet.addr ]] || { client_fail "missing /var/lib/curio/pdp_wallet.addr; curio provider bootstrap not complete"; return 1; }
  provider_native_addr="$(tr -d '\r\n' < /var/lib/curio/pdp_wallet.addr)"
  provider_private_key_hex="$(lotus wallet export "$provider_native_addr" | xxd -r -p | jq -r '.PrivateKey' | base64 -d | xxd -p -c 256 | tr -d '\r\n')"
  provider_eth_addr="$(cast wallet address --private-key "0x${provider_private_key_hex}" | tr -d '\r\n')"
  require_hex_address "Curio provider EVM address" "$provider_eth_addr"

  # Resolve provider id and approval/endorsement status for devnet-info.
  provider_id_raw="$(cast call "$service_registry_proxy_addr" "getProviderIdByAddress(address)(uint256)" "$provider_eth_addr" --rpc-url "$LOTUS_RPC_URL" | tr -d '\r\n' || true)"
  provider_id_dec="$(to_decimal "$provider_id_raw" || true)"
  [[ "$provider_id_dec" =~ ^[0-9]+$ ]] || {
    client_fail "unexpected provider id response: ${provider_id_raw:-<empty>}"
    return 1
  }
  provider_id="$provider_id_dec"
  if [[ "$provider_id" -le 0 ]]; then
    client_fail "provider id is not assigned yet for $provider_eth_addr"
    return 1
  fi

  is_approved_raw="$(cast call "$fwss_view_addr" "isProviderApproved(uint256)(bool)" "$provider_id" --rpc-url "$LOTUS_RPC_URL" | tr -d '\r\n')"
  is_endorsed_raw="$(cast call "$endorsements_addr" "containsProviderId(uint256)(bool)" "$provider_id" --rpc-url "$LOTUS_RPC_URL" | tr -d '\r\n')"
  [[ "$is_approved_raw" == "true" || "$is_approved_raw" == "false" ]] || { client_fail "invalid approval status response: $is_approved_raw"; return 1; }
  [[ "$is_endorsed_raw" == "true" || "$is_endorsed_raw" == "false" ]] || { client_fail "invalid endorsement status response: $is_endorsed_raw"; return 1; }
  is_approved_json="$is_approved_raw"
  is_endorsed_json="$is_endorsed_raw"

  # Write main file + compatibility copies used by existing tooling/scripts.
  mkdir -p "$(dirname "$DEVNET_INFO_FILE")"
  jq -n \
    --arg run_id "$run_id" \
    --arg start_time "$start_time" \
    --arg startup_duration "$startup_duration" \
    --arg user_name "$SYNAPSE_USER_NAME" \
    --arg user_evm "$user_evm_addr" \
    --arg user_native "$user_native_addr" \
    --arg user_priv "$user_private_key" \
    --arg multicall3_addr "$multicall3_addr" \
    --arg mockusdfc_addr "$usdfc_addr" \
    --arg fwss_service_proxy_addr "$fwss_proxy_addr" \
    --arg fwss_state_view_addr "$fwss_view_addr" \
    --arg fwss_impl_addr "$fwss_impl_addr" \
    --arg pdp_verifier_proxy_addr "$pdp_verifier_proxy_addr" \
    --arg pdp_verifier_impl_addr "$pdp_verifier_impl_addr" \
    --arg service_provider_registry_proxy_addr "$service_registry_proxy_addr" \
    --arg service_provider_registry_impl_addr "$service_registry_impl_addr" \
    --arg filecoin_pay_v1_addr "$filecoin_pay_addr" \
    --arg endorsements_addr "$endorsements_addr" \
    --arg session_key_registry_addr "$session_key_registry_addr" \
    --arg lotus_rpc_url "http://lotus:1234/rpc/v1" \
    --arg lotus_container_id "lotus" \
    --arg lotus_container_name "lotus" \
    --arg lotus_miner_container_id "lotus-miner" \
    --arg lotus_miner_container_name "lotus-miner" \
    --arg provider_eth "$provider_eth_addr" \
    --arg provider_native "$provider_native_addr" \
    --arg provider_url "http://curio:12310" \
    --arg provider_container_id "curio" \
    --arg provider_container_name "curio" \
    --arg yugabyte_web_ui_url "http://yugabyte:15433" \
    --argjson provider_id "$provider_id" \
    --argjson lotus_miner_api_port 2345 \
    --argjson is_approved "$is_approved_json" \
    --argjson is_endorsed "$is_endorsed_json" \
    '{
      version: 1,
      info: {
        run_id: $run_id,
        start_time: $start_time,
        startup_duration: $startup_duration,
        users: [
          {
            name: $user_name,
            evm_addr: $user_evm,
            native_addr: $user_native,
            private_key_hex: $user_priv
          }
        ],
        contracts: {
          multicall3_addr: $multicall3_addr,
          mockusdfc_addr: $mockusdfc_addr,
          fwss_service_proxy_addr: $fwss_service_proxy_addr,
          fwss_state_view_addr: $fwss_state_view_addr,
          fwss_impl_addr: $fwss_impl_addr,
          pdp_verifier_proxy_addr: $pdp_verifier_proxy_addr,
          pdp_verifier_impl_addr: $pdp_verifier_impl_addr,
          service_provider_registry_proxy_addr: $service_provider_registry_proxy_addr,
          service_provider_registry_impl_addr: $service_provider_registry_impl_addr,
          filecoin_pay_v1_addr: $filecoin_pay_v1_addr,
          endorsements_addr: $endorsements_addr,
          session_key_registry_addr: $session_key_registry_addr
        },
        lotus: {
          host_rpc_url: $lotus_rpc_url,
          container_id: $lotus_container_id,
          container_name: $lotus_container_name
        },
        lotus_miner: {
          container_id: $lotus_miner_container_id,
          container_name: $lotus_miner_container_name,
          api_port: $lotus_miner_api_port
        },
        pdp_sps: [
          {
            provider_id: $provider_id,
            eth_addr: $provider_eth,
            native_addr: $provider_native,
            pdp_service_url: $provider_url,
            container_id: $provider_container_id,
            container_name: $provider_container_name,
            is_approved: $is_approved,
            is_endorsed: $is_endorsed,
            yugabyte: {
              web_ui_url: $yugabyte_web_ui_url,
              master_rpc_port: 7100,
              ysql_port: 5433
            }
          }
        ]
      }
    }' > "$DEVNET_INFO_FILE"

  mkdir -p "$(dirname "$DEVNET_INFO_COMPAT_FILE")"
  cp "$DEVNET_INFO_FILE" "$DEVNET_INFO_COMPAT_FILE"
  cp "$DEVNET_INFO_FILE" "$CURIO_MK12_CLIENT_REPO/devnet-info.json"
  client_log "Wrote devnet info: $DEVNET_INFO_FILE"
}

# Configure on-chain user payment state for storage flows.
# Idempotent markers prevent re-running expensive txs on every restart.
ensure_user1_contract_setup() {
  client_log "Ensuring user contract balances and approvals"
  local lotus_rpc_url
  local user_evm_addr
  local user_private_key
  local deployer_private_key
  local usdfc_addr
  local filecoin_pay_addr
  local fwss_addr
  local user_balance_raw
  local user_balance_dec
  local allowance_raw
  local allowance_dec
  local usdfc_marker="$SYNAPSE_USER_DIR/.init.user1.usdfc"
  local payments_marker="$SYNAPSE_USER_DIR/.init.user1.payments"

  lotus_rpc_url="${LOTUS_RPC_URL:-http://lotus:1234/rpc/v1}"
  user_evm_addr="$(tr -d '\r\n[:space:]' < "$SYNAPSE_USER_EVM_FILE")"
  user_private_key="$(tr -d '\r\n[:space:]' < "$SYNAPSE_USER_PRIV_FILE")"
  deployer_private_key="$(tr -d '\r\n[:space:]' < "$DEPLOYER_PRIVATE_KEY_FILE")"

  [[ "$user_private_key" =~ ^0x[0-9a-fA-F]{64}$ ]] || { client_fail "invalid USER_1 private key format"; return 1; }
  [[ "$deployer_private_key" =~ ^0x[0-9a-fA-F]{64}$ ]] || { client_fail "invalid deployer private key format"; return 1; }
  require_hex_address "USER_1 EVM address" "$user_evm_addr" || return 1

  usdfc_addr="$(jq -r '.contracts.usdfc // empty' "$CONTRACT_ADDRESSES_FILE")"
  filecoin_pay_addr="$(jq -r '.contracts.filecoin_pay_v1 // empty' "$CONTRACT_ADDRESSES_FILE")"
  fwss_addr="$(jq -r '.contracts.filecoin_warm_storage_service_proxy // .contracts.filecoin_warm_storage_service // empty' "$CONTRACT_ADDRESSES_FILE")"

  [[ "$usdfc_addr" =~ ^0x[0-9a-fA-F]{40}$ ]] || usdfc_addr="${CURIO_DEVNET_USDFC_ADDRESS:-}"
  [[ "$filecoin_pay_addr" =~ ^0x[0-9a-fA-F]{40}$ ]] || filecoin_pay_addr="${CURIO_DEVNET_FILECOIN_PAY_ADDRESS:-}"
  [[ "$fwss_addr" =~ ^0x[0-9a-fA-F]{40}$ ]] || fwss_addr="${CURIO_DEVNET_FWSS_ADDRESS:-}"

  require_hex_address "USDFC" "$usdfc_addr" || return 1
  require_hex_address "FilecoinPay" "$filecoin_pay_addr" || return 1
  require_hex_address "FWSS" "$fwss_addr" || return 1

  # Ensure USER_1 has USDFC.
  user_balance_raw="$(cast call "$usdfc_addr" "balanceOf(address)(uint256)" "$user_evm_addr" --rpc-url "$lotus_rpc_url" | tr -d '\r\n' || true)"
  user_balance_dec="$(to_decimal "$user_balance_raw" || true)"
  if [[ ! "$user_balance_dec" =~ [1-9] ]]; then
    client_log "USER_1 USDFC balance is zero; funding USER_1 with $USER1_USDFC_FUND_AMOUNT_WEI"
    with_deployer_tx_lock cast send "$usdfc_addr" "transfer(address,uint256)" "$user_evm_addr" "$USER1_USDFC_FUND_AMOUNT_WEI" \
      --rpc-url "$lotus_rpc_url" \
      --private-key "$deployer_private_key" \
      --gas-limit 100000000 >/dev/null || return 1
    user_balance_raw="$(cast call "$usdfc_addr" "balanceOf(address)(uint256)" "$user_evm_addr" --rpc-url "$lotus_rpc_url" | tr -d '\r\n' || true)"
    user_balance_dec="$(to_decimal "$user_balance_raw" || true)"
    [[ "$user_balance_dec" =~ [1-9] ]] || { client_fail "USER_1 USDFC funding did not reflect on-chain"; return 1; }
  fi
  touch "$usdfc_marker"
  client_log "USER_1 USDFC balance is ready"

  # Configure FilecoinPay allowance, deposit and FWSS operator approval.
  if [[ ! -f "$payments_marker" ]]; then
    client_log "Configuring FilecoinPay allowance/deposit/operator approval for USER_1"
    allowance_raw="$(cast call "$usdfc_addr" "allowance(address,address)(uint256)" "$user_evm_addr" "$filecoin_pay_addr" --rpc-url "$lotus_rpc_url" | tr -d '\r\n' || true)"
    allowance_dec="$(to_decimal "$allowance_raw" || true)"
    if [[ ! "$allowance_dec" =~ [1-9] ]]; then
      cast send "$usdfc_addr" "approve(address,uint256)" "$filecoin_pay_addr" "$USER1_DEPOSIT_AMOUNT_WEI" \
        --rpc-url "$lotus_rpc_url" \
        --private-key "$user_private_key" \
        --gas-limit 100000000 >/dev/null || return 1
    fi

    cast send "$filecoin_pay_addr" "deposit(address,address,uint256)" "$usdfc_addr" "$user_evm_addr" "$USER1_DEPOSIT_AMOUNT_WEI" \
      --rpc-url "$lotus_rpc_url" \
      --private-key "$user_private_key" \
      --gas-limit 100000000 >/dev/null || return 1

    cast send "$filecoin_pay_addr" "setOperatorApproval(address,address,bool,uint256,uint256,uint256)" \
      "$usdfc_addr" "$fwss_addr" "true" \
      "115792089237316195423570985008687907853269984665640564039457584007913129639935" \
      "115792089237316195423570985008687907853269984665640564039457584007913129639935" \
      "$USER1_LOCKUP_EPOCHS" \
      --rpc-url "$lotus_rpc_url" \
      --private-key "$user_private_key" \
      --gas-limit 100000000 >/dev/null || return 1

    touch "$payments_marker"
  fi
  client_log "USER_1 payment setup is ready"
}

# Keep existing mk12 bootstrap flow (client wallet, FIL+, datacap) with markers.
run_mk12_bootstrap() {
  if [ ! -f "$CURIO_MK12_CLIENT_REPO/.init" ]; then
    echo "Initialising mk12 client"
    sptool --actor t01000 toolbox mk12-client init
    touch "$CURIO_MK12_CLIENT_REPO/.init"
  fi

  # mk12 wallet initialization and FIL market balance.
  if [ ! -f "$CURIO_MK12_CLIENT_REPO/.init.wallet" ]; then
    echo "Setting up client wallet"
    def_wallet="$(sptool --actor t01000 toolbox mk12-client wallet default)"
    lotus send "$def_wallet" 100
    sleep 10
    sptool --actor t01000 toolbox mk12-client market-add -y 10
    touch "$CURIO_MK12_CLIENT_REPO/.init.wallet"
  fi

  # FIL+ notary bootstrap and multisig approval flow.
  if [ ! -f "$CURIO_MK12_CLIENT_REPO/.init.filplus" ]; then
    echo "Setting up FIL+ wallets"
    ROOT_KEY_1="$(cat "$LOTUS_PATH/rootkey-1")"
    ROOT_KEY_2="$(cat "$LOTUS_PATH/rootkey-2")"
    echo "Root key 1: $ROOT_KEY_1"
    echo "Root key 2: $ROOT_KEY_2"
    lotus wallet import "$LOTUS_PATH/bls-$ROOT_KEY_1.keyinfo"
    lotus wallet import "$LOTUS_PATH/bls-$ROOT_KEY_2.keyinfo"
    NOTARY_1="$(lotus wallet new secp256k1)"
    NOTARY_2="$(lotus wallet new secp256k1)"
    echo "$NOTARY_1" > "$CURIO_MK12_CLIENT_REPO/notary_1"
    echo "$NOTARY_2" > "$CURIO_MK12_CLIENT_REPO/notary_2"
    echo "Notary 1: $NOTARY_1"
    echo "Notary 2: $NOTARY_2"

    echo "Add verifier root_key_1 notary_1"
    lotus-shed verifreg add-verifier "$ROOT_KEY_1" "$NOTARY_1" 1000000000000
    sleep 15
    echo "Msig inspect t080"
    lotus msig inspect t080
    PARAMS="$(lotus msig inspect t080 | tail -1 | awk '{print $8}')"
    echo "Params: $PARAMS"
    echo "Msig approve"
    lotus msig approve --from="$ROOT_KEY_2" t080 0 t0100 t06 0 2 "$PARAMS"

    echo "Send 10 FIL to NOTARY_1"
    lotus send "$NOTARY_1" 10
    touch "$CURIO_MK12_CLIENT_REPO/.init.filplus"
    sleep 10
  fi

  # Grant datacap to mk12 default wallet.
  if [ ! -f "$CURIO_MK12_CLIENT_REPO/.init.datacap" ]; then
    notary="$(lotus filplus list-notaries | awk '$2 != 0 {print $1}' | awk -F':' '{print $1}')"
    def_wallet="$(sptool --actor t01000 toolbox mk12-client wallet default)"
    lotus filplus grant-datacap --from "$notary" "$def_wallet" 1000000000
    touch "$CURIO_MK12_CLIENT_REPO/.init.datacap"
  fi
}

# One contract/bootstrap attempt for piece-server client side.
# mk12 bootstrap is handled separately in foreground before piece-server starts.
bootstrap_client_once() {
  local provider_ready_file="/var/lib/curio/provider.ready"

  client_log "Starting client bootstrap attempt"
  wait_for_contract_artifacts || { client_fail "contract artifacts are not ready"; return 1; }
  # shellcheck disable=SC1090
  source "$CONTRACT_ENV_FILE"
  ensure_synapse_user || return 1
  ensure_user1_contract_setup || return 1
  [[ -f "$provider_ready_file" ]] || {
    client_log "Server-side provider bootstrap is not complete yet; waiting for $provider_ready_file"
    return 1
  }
  write_devnet_info || return 1
  date -u +'%Y-%m-%dT%H:%M:%SZ' > "$CLIENT_READY_FILE" || { client_fail "failed to write readiness file $CLIENT_READY_FILE"; return 1; }
  client_log "Client bootstrap done; wrote readiness file $CLIENT_READY_FILE"
}

# Background bootstrap worker with lock + bounded retries.
bootstrap_client_background() {
  local attempts=0
  mkdir -p "$(dirname "$CLIENT_READY_FILE")"
  if command -v flock >/dev/null 2>&1; then
    exec 9>"$CLIENT_LOCK_FILE"
    if ! flock -n 9; then
      echo "Client bootstrap worker already running; skipping duplicate worker"
      return 0
    fi
  fi

  while true; do
    attempts=$((attempts + 1))
    client_log "Attempt $attempts"
    if bootstrap_client_once; then
      echo "Client bootstrap complete"
      return 0
    fi
    if [[ "$CLIENT_BOOTSTRAP_MAX_RETRIES" -gt 0 && "$attempts" -ge "$CLIENT_BOOTSTRAP_MAX_RETRIES" ]]; then
      echo "Client bootstrap failed after $attempts attempts"
      return 1
    fi
    echo "Client bootstrap not ready yet; retrying in ${CLIENT_BOOTSTRAP_RETRY_INTERVAL_SEC}s..."
    sleep "$CLIENT_BOOTSTRAP_RETRY_INTERVAL_SEC"
  done
}

# Setup synapse-sdk workspace used by manual e2e examples.
# Runs independently from chain/bootstrap state and can execute in parallel.
bootstrap_synapse_sdk_once() {
  local ts

  if [[ -f "$SYNAPSE_SDK_READY_FILE" ]]; then
    synapse_log "Ready file already present; skipping synapse-sdk bootstrap"
    return 0
  fi

  mkdir -p "/var/lib/curio-client"

  if command -v flock >/dev/null 2>&1; then
    exec 7>"$SYNAPSE_SDK_LOCK_FILE"
    flock 7
  fi

  # Re-check under lock so duplicate workers remain idempotent.
  if [[ -f "$SYNAPSE_SDK_READY_FILE" ]]; then
    synapse_log "Ready file created by another worker; skipping"
    return 0
  fi

  synapse_log "Preparing synapse-sdk in /var/lib/curio-client"
  if [[ -d "$SYNAPSE_SDK_REPO_DIR/.git" ]]; then
    synapse_log "Existing synapse-sdk checkout found; reusing"
  elif [[ -d "$SYNAPSE_SDK_REPO_DIR" ]]; then
    synapse_fail "$SYNAPSE_SDK_REPO_DIR exists but is not a git checkout"
    return 1
  else
    git clone "$SYNAPSE_SDK_REPO_URL" "$SYNAPSE_SDK_REPO_DIR" || { synapse_fail "git clone failed"; return 1; }
  fi

  (
    cd "$SYNAPSE_SDK_REPO_DIR" || exit 1
    npm install -g pnpm@latest-10
    pnpm install
    pnpm build
  ) || { synapse_fail "pnpm bootstrap failed"; return 1; }

  ts="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  printf '%s\n' "$ts" > "$SYNAPSE_SDK_READY_FILE" || { synapse_fail "failed to write readiness file"; return 1; }
  synapse_log "Synapse SDK bootstrap complete; wrote $SYNAPSE_SDK_READY_FILE"
}

bootstrap_synapse_sdk_background() {
  synapse_log "Starting background bootstrap"
  bootstrap_synapse_sdk_once
}

# Wait for Lotus and chain head threshold before starting any mutable setup.
echo Wait for lotus is ready ...
lotus wait-api
head=0
# Loop until the head is greater than 9
while [[ $head -le 9 ]]; do
    head=$(lotus chain list | awk '{print $1}' | awk -F':' '{print $1}' | tail -1)
    if [[ $head -le 9 ]]; then
        echo "Current head: $head, which is not greater than 9. Waiting..."
        sleep 1  # Wait for 4 seconds before checking again
    else
        echo "The head is now greater than 9: $head"
    fi
done

# Resolve piece-server bind IP from docker DNS.
myip=`nslookup piece-server | grep -v "#" | grep Address | awk '{print $2}'`

echo $LOTUS_PATH

# Ensure the scanning directory exists
SCAN_DIR="/var/lib/curio-client/data"
if [ ! -d "$SCAN_DIR" ]; then
    echo "Creating scan directory: $SCAN_DIR"
    mkdir -p "$SCAN_DIR"
    mkdir -p /var/lib/curio-client/public
fi

# Start contract/bootstrap worker in parallel first.
echo "Starting client contract bootstrap worker ..."
bootstrap_client_background &
CLIENT_BOOTSTRAP_PID=$!

echo "Starting synapse-sdk bootstrap worker ..."
bootstrap_synapse_sdk_background &
SYNAPSE_BOOTSTRAP_PID=$!

echo "Running mk12 bootstrap (blocking startup until complete) ..."
if ! run_mk12_bootstrap; then
  echo "mk12 bootstrap failed; stopping background workers"
  kill -15 "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1 || true
  kill -15 "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1 || true
  wait "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1 || true
  wait "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1 || true
  exit 1
fi
echo "mk12 bootstrap complete"

CLIENT_BOOTSTRAP_DONE=0
SYNAPSE_BOOTSTRAP_DONE=0

# If contract bootstrap already failed while mk12 was running, fail now.
if ! kill -0 "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1; then
  wait "$CLIENT_BOOTSTRAP_PID" || CLIENT_BOOTSTRAP_RC=$?
  CLIENT_BOOTSTRAP_RC="${CLIENT_BOOTSTRAP_RC:-0}"
  if [[ "$CLIENT_BOOTSTRAP_RC" -ne 0 ]]; then
    echo "Client contract bootstrap failed before piece-server start"
    kill -15 "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1 || true
    wait "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1 || true
    exit "$CLIENT_BOOTSTRAP_RC"
  fi
  CLIENT_BOOTSTRAP_DONE=1
fi

if ! kill -0 "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1; then
  wait "$SYNAPSE_BOOTSTRAP_PID" || SYNAPSE_BOOTSTRAP_RC=$?
  SYNAPSE_BOOTSTRAP_RC="${SYNAPSE_BOOTSTRAP_RC:-0}"
  if [[ "$SYNAPSE_BOOTSTRAP_RC" -ne 0 ]]; then
    echo "Synapse SDK bootstrap failed before piece-server start"
    kill -15 "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1 || true
    wait "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1 || true
    exit "$SYNAPSE_BOOTSTRAP_RC"
  fi
  SYNAPSE_BOOTSTRAP_DONE=1
fi

# Start piece-server in HTTP mode after mk12 bootstrap completes.
echo "Starting piece-server on localhost:12320 scanning $SCAN_DIR"
piece-server run --dir="$SCAN_DIR" --port=12320 --bind="$myip" &
PIECE_SERVER_PID=$!

# If piece-server exits first, propagate that exit code.
while true; do
  if ! kill -0 "$PIECE_SERVER_PID" >/dev/null 2>&1; then
    wait "$PIECE_SERVER_PID"
    exit $?
  fi

  if [[ "$CLIENT_BOOTSTRAP_DONE" -eq 0 ]] && ! kill -0 "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1; then
    wait "$CLIENT_BOOTSTRAP_PID" || CLIENT_BOOTSTRAP_RC=$?
    CLIENT_BOOTSTRAP_RC="${CLIENT_BOOTSTRAP_RC:-0}"
    if [[ "$CLIENT_BOOTSTRAP_RC" -ne 0 ]]; then
      echo "Client bootstrap failed; stopping piece-server"
      kill -15 "$PIECE_SERVER_PID" >/dev/null 2>&1 || true
      kill -15 "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1 || true
      wait "$PIECE_SERVER_PID" >/dev/null 2>&1 || true
      wait "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1 || true
      exit "$CLIENT_BOOTSTRAP_RC"
    fi
    CLIENT_BOOTSTRAP_DONE=1
    echo "Client contract bootstrap completed"
  fi

  if [[ "$SYNAPSE_BOOTSTRAP_DONE" -eq 0 ]] && ! kill -0 "$SYNAPSE_BOOTSTRAP_PID" >/dev/null 2>&1; then
    wait "$SYNAPSE_BOOTSTRAP_PID" || SYNAPSE_BOOTSTRAP_RC=$?
    SYNAPSE_BOOTSTRAP_RC="${SYNAPSE_BOOTSTRAP_RC:-0}"
    if [[ "$SYNAPSE_BOOTSTRAP_RC" -ne 0 ]]; then
      echo "Synapse SDK bootstrap failed; stopping piece-server"
      kill -15 "$PIECE_SERVER_PID" >/dev/null 2>&1 || true
      kill -15 "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1 || true
      wait "$PIECE_SERVER_PID" >/dev/null 2>&1 || true
      wait "$CLIENT_BOOTSTRAP_PID" >/dev/null 2>&1 || true
      exit "$SYNAPSE_BOOTSTRAP_RC"
    fi
    SYNAPSE_BOOTSTRAP_DONE=1
    echo "Synapse SDK bootstrap completed"
  fi

  if [[ "$CLIENT_BOOTSTRAP_DONE" -eq 1 && "$SYNAPSE_BOOTSTRAP_DONE" -eq 1 ]]; then
    break
  fi
  sleep 1
done

# Bootstrap succeeded; keep container lifecycle tied to piece-server process.
wait "$PIECE_SERVER_PID"
