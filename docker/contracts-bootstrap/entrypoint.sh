#!/usr/bin/env bash
set -euo pipefail

log() {
  echo "[$(date -u +'%Y-%m-%dT%H:%M:%SZ')] $*"
}

die() {
  log "ERROR: $*"
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "required command not found: $cmd"
}

extract_address_from_forge_output() {
  local output="$1"
  local addr
  addr="$(echo "$output" | awk '/Deployed to/ {print $3}' | tail -n1 | tr -d '\r\n')"
  if [[ -z "$addr" ]]; then
    addr="$(echo "$output" | jq -r '.deployedTo // .deployed_to // empty' 2>/dev/null || true)"
  fi
  [[ "$addr" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "could not parse deployed address from forge output"
  echo "$addr"
}

clone_with_ref() {
  local repo="$1"
  local ref="$2"
  local target_dir="$3"

  rm -rf "$target_dir"
  git clone --depth 1 "$repo" "$target_dir"

  if [[ -z "$ref" ]]; then
    return 0
  fi

  if git -C "$target_dir" fetch --depth 1 origin "$ref" >/dev/null 2>&1; then
    git -C "$target_dir" checkout --detach FETCH_HEAD >/dev/null 2>&1
    return 0
  fi

  if git -C "$target_dir" rev-parse --verify "$ref^{commit}" >/dev/null 2>&1; then
    git -C "$target_dir" checkout --detach "$ref" >/dev/null 2>&1
    return 0
  fi

  die "failed to checkout ref '$ref' from '$repo'"
}

stage_local_source() {
  local source_dir="$1"
  local target_dir="$2"
  local label="$3"

  [[ -d "$source_dir" ]] || die "local $label directory not found: $source_dir"
  rm -rf "$target_dir"
  cp -a "$source_dir" "$target_dir"
}

wait_for_lotus() {
  local tries=0
  while true; do
    if lotus wait-api >/dev/null 2>&1; then
      break
    fi
    tries=$((tries + 1))
    if (( tries > 120 )); then
      die "lotus API did not become ready in time"
    fi
    sleep 1
  done
}

main() {
  require_cmd git
  require_cmd jq
  require_cmd cast
  require_cmd forge
  require_cmd lotus
  require_cmd base64
  require_cmd xxd

  local contracts_dir="${CONTRACTS_DIR:-/var/lib/contracts}"
  local work_dir="${WORK_DIR:-/tmp/contracts-bootstrap}"
  local env_file="${CONTRACT_ENV_FILE:-$contracts_dir/curio-devnet.env}"
  local addresses_file="${CONTRACT_ADDRESSES_FILE:-$contracts_dir/contract_addresses.json}"
  local log_file="${CONTRACT_BOOTSTRAP_LOG_FILE:-$contracts_dir/contracts-bootstrap.log}"
  local force_redeploy="${FORCE_REDEPLOY:-false}"

  local lotus_rpc_url="${LOTUS_RPC_URL:-http://lotus:1234/rpc/v1}"
  local chain_id="${CHAIN_ID:-31415926}"

  local filecoin_services_repo="${FILECOIN_SERVICES_REPO:-https://github.com/FilOzone/filecoin-services.git}"
  local filecoin_services_ref="${FILECOIN_SERVICES_REF:-main}"
  local multicall3_repo="${MULTICALL3_REPO:-https://github.com/mds1/multicall3.git}"
  local multicall3_ref="${MULTICALL3_REF:-main}"
  local contract_source_mode="${CONTRACT_SOURCE_MODE:-remote}"
  local filecoin_services_local_path="${FILECOIN_SERVICES_LOCAL_PATH:-/opt/local-src/filecoin-services}"
  local multicall3_local_path="${MULTICALL3_LOCAL_PATH:-/opt/local-src/multicall3}"

  local service_name="${CONTRACT_SERVICE_NAME:-curio-devnet}"
  local service_description="${CONTRACT_SERVICE_DESCRIPTION:-Curio devnet warm storage service}"
  local key_password="${CONTRACT_DEPLOYER_PASSWORD:-devnet}"
  local deployer_fund_amount_fil="${DEPLOYER_FUND_AMOUNT_FIL:-5000}"
  local genesis_preseal_key="${GENESIS_PRESEAL_KEY:-/var/lib/genesis/pre-seal-t01000.key}"

  mkdir -p "$contracts_dir" "$work_dir"
  : > "$log_file"
  exec > >(tee -a "$log_file") 2>&1

  log "Starting contracts bootstrap"
  log "LOTUS_RPC_URL=$lotus_rpc_url"
  log "FILECOIN_SERVICES_REPO=$filecoin_services_repo @ ${filecoin_services_ref:-<default>}"
  log "MULTICALL3_REPO=$multicall3_repo @ ${multicall3_ref:-<default>}"
  log "CONTRACT_SOURCE_MODE=$contract_source_mode"

  if [[ "$contract_source_mode" != "local" && "$contract_source_mode" != "remote" ]]; then
    die "unsupported CONTRACT_SOURCE_MODE '$contract_source_mode' (expected 'local' or 'remote')"
  fi

  if [[ -f "$env_file" && "$force_redeploy" != "true" ]]; then
    log "Contract env already exists at $env_file (FORCE_REDEPLOY != true); skipping bootstrap."
    exit 0
  fi

  wait_for_lotus

  local rpc_chain_id
  rpc_chain_id="$(cast chain-id --rpc-url "$lotus_rpc_url" | tr -d '\r\n')"
  [[ -n "$rpc_chain_id" ]] || die "failed to read chain id from RPC"
  if [[ "$chain_id" != "$rpc_chain_id" ]]; then
    log "Configured CHAIN_ID=$chain_id does not match RPC chain id=$rpc_chain_id; using RPC value."
    chain_id="$rpc_chain_id"
  fi

  local deployer_fil_address
  local deployer_eth_address
  local deployer_private_key
  local deployer_key_file="$contracts_dir/deployer.private-key"
  local deployer_fil_file="$contracts_dir/deployer.fil-address"
  local deployer_eth_file="$contracts_dir/deployer.eth-address"

  if [[ -s "$deployer_key_file" && -s "$deployer_fil_file" ]]; then
    deployer_private_key="$(tr -d '\r\n' < "$deployer_key_file")"
    deployer_fil_address="$(tr -d '\r\n' < "$deployer_fil_file")"
    [[ "$deployer_private_key" =~ ^0x[0-9a-fA-F]{64}$ ]] || die "stored deployer private key is invalid"
    log "Reusing existing deployer key ($deployer_fil_address)"
  else
    log "Creating deployer key"
    deployer_fil_address="$(lotus wallet new secp256k1 | tail -n1 | tr -d '\r\n')"
    [[ -n "$deployer_fil_address" ]] || die "failed to create deployer wallet"

    local key_export_raw key_export privkey_b64 privkey_hex
    key_export_raw="$(lotus wallet export "$deployer_fil_address" | tr -d '\r\n')"
    if [[ "$key_export_raw" =~ ^[0-9a-fA-F]+$ ]]; then
      key_export="$(printf '%s' "$key_export_raw" | xxd -r -p)"
    else
      key_export="$key_export_raw"
    fi
    privkey_b64="$(echo "$key_export" | jq -r '.PrivateKey // .privateKey // .KeyInfo.PrivateKey // .keyinfo.PrivateKey // empty')"
    [[ -n "$privkey_b64" ]] || die "failed to extract private key from lotus wallet export"

    privkey_hex="$(printf '%s' "$privkey_b64" | base64 -d | xxd -p -c 256 | tr -d '\r\n')"
    [[ "$privkey_hex" =~ ^[0-9a-fA-F]{64}$ ]] || die "deployer private key format is invalid"
    deployer_private_key="0x$privkey_hex"

    printf '%s\n' "$deployer_private_key" > "$deployer_key_file"
    chmod 600 "$deployer_key_file"
    printf '%s\n' "$deployer_fil_address" > "$deployer_fil_file"
  fi

  deployer_eth_address="$(cast wallet address --private-key "$deployer_private_key" | tr -d '\r\n')"
  [[ "$deployer_eth_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "failed to derive deployer EVM address"
  printf '%s\n' "$deployer_eth_address" > "$deployer_eth_file"

  local default_wallet fund_cid default_wallet_balance wallet_wait_tries
  default_wallet=""
  default_wallet_balance=""
  wallet_wait_tries=0

  log "Waiting for default wallet to have spendable balance..."
  while true; do
    default_wallet="$(lotus wallet default 2>/dev/null | tr -d '\r\n' || true)"
    default_wallet_balance="$(lotus wallet balance "$default_wallet" 2>/dev/null | awk '{print $1}' | tr -d '\r\n' || true)"

    if [[ -n "$default_wallet" && -n "$default_wallet_balance" && "$default_wallet_balance" != "0" && "$default_wallet_balance" != "0.0" ]]; then
      break
    fi

    wallet_wait_tries=$((wallet_wait_tries + 1))
    if (( wallet_wait_tries > 180 )); then
      die "default wallet did not get spendable balance in time"
    fi
    sleep 1
  done

  log "Funding deployer EVM wallet $deployer_eth_address from $default_wallet with $deployer_fund_amount_fil FIL"
  fund_cid="$(lotus send --from "$default_wallet" "$deployer_eth_address" "$deployer_fund_amount_fil" | awk '/^bafy/{print $1}' | tail -n1 | tr -d '\r\n')"
  [[ -n "$fund_cid" ]] || die "failed to fund deployer wallet"
  lotus state wait-msg "$fund_cid" >/dev/null

  local keystore_dir="$work_dir/keystore"
  rm -rf "$keystore_dir"
  mkdir -p "$keystore_dir"
  cast wallet import \
    --keystore-dir "$keystore_dir" \
    --unsafe-password "$key_password" \
    --private-key "$deployer_private_key" \
    bootstrap-deployer >/dev/null

  local eth_keystore
  eth_keystore="$(find "$keystore_dir" -maxdepth 1 -type f | head -n1)"
  [[ -f "$eth_keystore" ]] || die "failed to create deployer keystore"

  local filecoin_services_dir="$work_dir/filecoin-services"
  if [[ "$contract_source_mode" == "local" ]]; then
    stage_local_source "$filecoin_services_local_path" "$filecoin_services_dir" "filecoin-services"
  else
    clone_with_ref "$filecoin_services_repo" "$filecoin_services_ref" "$filecoin_services_dir"
    git -C "$filecoin_services_dir" submodule update --init --recursive
  fi

  pushd "$filecoin_services_dir/service_contracts" >/dev/null
  if [[ "$contract_source_mode" == "local" ]]; then
    [[ -f "lib/pdp/src/PDPVerifier.sol" ]] || die "local filecoin-services checkout is missing lib/pdp (run submodule update in local source)"
    [[ -f "lib/fws-payments/src/FilecoinPayV1.sol" ]] || die "local filecoin-services checkout is missing lib/fws-payments (run submodule update in local source)"
  else
    make install
  fi

  log "Deploying MockUSDFC"
  local usdfc_deploy_output usdfc_address
  if ! usdfc_deploy_output="$(forge create \
      test/mocks/SharedMocks.sol:MockERC20 \
      --broadcast \
      --rpc-url "$lotus_rpc_url" \
      --private-key "$deployer_private_key" 2>&1)"; then
    echo "$usdfc_deploy_output"
    die "failed to deploy MockUSDFC"
  fi
  echo "$usdfc_deploy_output"
  usdfc_address="$(extract_address_from_forge_output "$usdfc_deploy_output")"
  log "MockUSDFC deployed: $usdfc_address"

  log "Deploying Filecoin services contracts"

  export ETH_RPC_URL="$lotus_rpc_url"
  export ETH_KEYSTORE="$eth_keystore"
  export PASSWORD="$key_password"
  export CHAIN="$chain_id"
  export DRY_RUN=false
  export AUTO_VERIFY=false
  export SERVICE_NAME="$service_name"
  export SERVICE_DESCRIPTION="$service_description"
  export USDFC_TOKEN_ADDRESS="$usdfc_address"

  local deploy_script="./tools/warm-storage-deploy-all.sh"
  if [[ ! -f "$deploy_script" ]]; then
    deploy_script="./tools/deploy-all-warm-storage.sh"
  fi
  [[ -f "$deploy_script" ]] || die "filecoin-services deploy script not found (expected tools/warm-storage-deploy-all.sh or tools/deploy-all-warm-storage.sh)"
  bash "$deploy_script"

  local pdp_verifier_proxy_address
  local pdp_verifier_implementation_address
  local fwss_proxy_address
  local fwss_implementation_address
  local fwss_view_address
  local service_registry_proxy_address
  local service_registry_implementation_address
  local filecoin_pay_address
  local session_key_registry_address
  local endorsements_address
  pdp_verifier_proxy_address="$(jq -r --arg chain "$chain_id" '.[$chain].PDP_VERIFIER_PROXY_ADDRESS // empty' deployments.json)"
  pdp_verifier_implementation_address="$(jq -r --arg chain "$chain_id" '.[$chain].PDP_VERIFIER_IMPLEMENTATION_ADDRESS // empty' deployments.json)"
  fwss_proxy_address="$(jq -r --arg chain "$chain_id" '.[$chain].FWSS_PROXY_ADDRESS // empty' deployments.json)"
  fwss_implementation_address="$(jq -r --arg chain "$chain_id" '.[$chain].FWSS_IMPLEMENTATION_ADDRESS // empty' deployments.json)"
  fwss_view_address="$(jq -r --arg chain "$chain_id" '.[$chain].FWSS_VIEW_ADDRESS // empty' deployments.json)"
  service_registry_proxy_address="$(jq -r --arg chain "$chain_id" '.[$chain].SERVICE_PROVIDER_REGISTRY_PROXY_ADDRESS // empty' deployments.json)"
  service_registry_implementation_address="$(jq -r --arg chain "$chain_id" '.[$chain].SERVICE_PROVIDER_REGISTRY_IMPLEMENTATION_ADDRESS // empty' deployments.json)"
  filecoin_pay_address="$(jq -r --arg chain "$chain_id" '.[$chain].FILECOIN_PAY_ADDRESS // empty' deployments.json)"
  session_key_registry_address="$(jq -r --arg chain "$chain_id" '.[$chain].SESSION_KEY_REGISTRY_ADDRESS // empty' deployments.json)"
  endorsements_address="$(jq -r --arg chain "$chain_id" '.[$chain].ENDORSEMENT_SET_ADDRESS // empty' deployments.json)"

  [[ "$pdp_verifier_proxy_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing PDP verifier proxy address in deployments.json"
  [[ "$pdp_verifier_implementation_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing PDP verifier implementation address in deployments.json"
  [[ "$fwss_proxy_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing FWSS proxy address in deployments.json"
  [[ "$fwss_implementation_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing FWSS implementation address in deployments.json"
  [[ "$fwss_view_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing FWSS view address in deployments.json"
  [[ "$service_registry_proxy_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing Service Registry proxy address in deployments.json"
  [[ "$service_registry_implementation_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing Service Registry implementation address in deployments.json"
  [[ "$filecoin_pay_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing FilecoinPay address in deployments.json"
  [[ "$session_key_registry_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing SessionKeyRegistry address in deployments.json"
  [[ "$endorsements_address" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "missing Endorsements address in deployments.json"

  local filecoin_services_commit
  filecoin_services_commit="$(git -C "$filecoin_services_dir" rev-parse HEAD 2>/dev/null | tr -d '\r\n' || true)"
  if [[ -z "$filecoin_services_commit" ]]; then
    filecoin_services_commit="unknown"
  fi
  popd >/dev/null

  local multicall3_dir="$work_dir/multicall3"
  if [[ "$contract_source_mode" == "local" ]]; then
    stage_local_source "$multicall3_local_path" "$multicall3_dir" "multicall3"
  else
    clone_with_ref "$multicall3_repo" "$multicall3_ref" "$multicall3_dir"
  fi

  pushd "$multicall3_dir" >/dev/null
  log "Deploying Multicall3"
  local multicall_deploy_output multicall3_address
  if ! multicall_deploy_output="$(forge create \
      src/Multicall3.sol:Multicall3 \
      --broadcast \
      --rpc-url "$lotus_rpc_url" \
      --private-key "$deployer_private_key" 2>&1)"; then
    echo "$multicall_deploy_output"
    die "failed to deploy Multicall3"
  fi
  echo "$multicall_deploy_output"
  multicall3_address="$(extract_address_from_forge_output "$multicall_deploy_output")"
  local multicall3_commit
  multicall3_commit="$(git rev-parse HEAD 2>/dev/null | tr -d '\r\n' || true)"
  if [[ -z "$multicall3_commit" ]]; then
    multicall3_commit="unknown"
  fi
  popd >/dev/null

  jq -n \
    --arg generated_at "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" \
    --arg chain_id "$chain_id" \
    --arg deployer_fil "$deployer_fil_address" \
    --arg deployer_eth "$deployer_eth_address" \
    --arg pdp_verifier "$pdp_verifier_proxy_address" \
    --arg pdp_verifier_impl "$pdp_verifier_implementation_address" \
    --arg fwss "$fwss_proxy_address" \
    --arg fwss_impl "$fwss_implementation_address" \
    --arg fwss_view "$fwss_view_address" \
    --arg service_registry "$service_registry_proxy_address" \
    --arg service_registry_impl "$service_registry_implementation_address" \
    --arg filecoin_pay "$filecoin_pay_address" \
    --arg session_key_registry "$session_key_registry_address" \
    --arg endorsements "$endorsements_address" \
    --arg usdfc "$usdfc_address" \
    --arg multicall3 "$multicall3_address" \
    --arg fs_repo "$filecoin_services_repo" \
    --arg fs_ref "$filecoin_services_ref" \
    --arg fs_commit "$filecoin_services_commit" \
    --arg mc_repo "$multicall3_repo" \
    --arg mc_ref "$multicall3_ref" \
    --arg mc_commit "$multicall3_commit" \
    '{
      generated_at: $generated_at,
      chain_id: $chain_id,
      deployer: {
        filecoin_address: $deployer_fil,
        evm_address: $deployer_eth
      },
      contracts: {
        pdp_verifier: $pdp_verifier,
        pdp_verifier_proxy: $pdp_verifier,
        pdp_verifier_implementation: $pdp_verifier_impl,
        filecoin_warm_storage_service: $fwss,
        filecoin_warm_storage_service_proxy: $fwss,
        filecoin_warm_storage_service_implementation: $fwss_impl,
        filecoin_warm_storage_service_state_view: $fwss_view,
        service_provider_registry: $service_registry,
        service_provider_registry_proxy: $service_registry,
        service_provider_registry_implementation: $service_registry_impl,
        filecoin_pay_v1: $filecoin_pay,
        session_key_registry: $session_key_registry,
        endorsements: $endorsements,
        usdfc: $usdfc,
        multicall3: $multicall3
      },
      sources: {
        filecoin_services: {
          repo: $fs_repo,
          ref: $fs_ref,
          commit: $fs_commit
        },
        multicall3: {
          repo: $mc_repo,
          ref: $mc_ref,
          commit: $mc_commit
        }
      }
    }' > "$addresses_file"

  cat > "$env_file" <<EOF
# Generated by docker/contracts-bootstrap/entrypoint.sh on $(date -u +'%Y-%m-%dT%H:%M:%SZ')
export CURIO_DEVNET_PDP_VERIFIER_ADDRESS="$pdp_verifier_proxy_address"
export CURIO_DEVNET_PDP_VERIFIER_IMPLEMENTATION_ADDRESS="$pdp_verifier_implementation_address"
export CURIO_DEVNET_FWSS_ADDRESS="$fwss_proxy_address"
export CURIO_DEVNET_FWSS_IMPLEMENTATION_ADDRESS="$fwss_implementation_address"
export CURIO_DEVNET_FWSS_STATE_VIEW_ADDRESS="$fwss_view_address"
export CURIO_DEVNET_SERVICE_REGISTRY_ADDRESS="$service_registry_proxy_address"
export CURIO_DEVNET_SERVICE_REGISTRY_IMPLEMENTATION_ADDRESS="$service_registry_implementation_address"
export CURIO_DEVNET_FILECOIN_PAY_ADDRESS="$filecoin_pay_address"
export CURIO_DEVNET_SESSION_KEY_REGISTRY_ADDRESS="$session_key_registry_address"
export CURIO_DEVNET_ENDORSEMENTS_ADDRESS="$endorsements_address"
export CURIO_DEVNET_USDFC_ADDRESS="$usdfc_address"
export CURIO_DEVNET_MULTICALL3_ADDRESS="$multicall3_address"
EOF

  chmod 0644 "$env_file"
  chmod 0644 "$addresses_file"

  log "Contract bootstrap completed"
  log "Env file: $env_file"
  log "Addresses file: $addresses_file"
}

main "$@"
