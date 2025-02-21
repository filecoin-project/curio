#!/usr/bin/env bash
set -e
CURIO_MK12_CLIENT_REPO=$CURIO_MK12_CLIENT_REPO
echo Wait for lotus is ready ...
lotus wait-api
head=0
# Loop until the head is greater than 9
while [[ $head -le 15 ]]; do
    head=$(lotus chain list | awk '{print $1}' | awk -F':' '{print $1}' | tail -1)
    if [[ $head -le 15 ]]; then
        echo "Current head: $head, which is not greater than 15. Waiting..."
        sleep 1  # Wait for 4 seconds before checking again
    else
        echo "The head is now greater than 15: $head"
    fi
done

myip=`nslookup piece-server | grep -v "#" | grep Address | awk '{print $2}'`

echo $LOTUS_PATH

# Init mk12 client
 if [ ! -f $CURIO_MK12_CLIENT_REPO/.init ]; then
  echo "Initialising mk12 client"
  sptool --actor t01000 toolbox mk12-client init
  touch $CURIO_MK12_CLIENT_REPO/.init
fi

if [ ! -f $CURIO_MK12_CLIENT_REPO/.init.wallet ]; then
  echo "Setting up client wallet"
  def_wallet=$(sptool --actor t01000 toolbox mk12-client wallet default)
  lotus send $def_wallet 100
  sleep 10
  sptool --actor t01000 toolbox mk12-client market-add -y 10
  touch $CURIO_MK12_CLIENT_REPO/.init.wallet
fi

## Setup datacap notary
if [ ! -f $CURIO_MK12_CLIENT_REPO/.init.filplus ]; then
  echo Setting up FIL+ wallets
  ROOT_KEY_1=`cat $LOTUS_PATH/rootkey-1`
  ROOT_KEY_2=`cat $LOTUS_PATH/rootkey-2`
  echo Root key 1: $ROOT_KEY_1
  echo Root key 2: $ROOT_KEY_2
  lotus wallet import $LOTUS_PATH/bls-$ROOT_KEY_1.keyinfo
  lotus wallet import $LOTUS_PATH/bls-$ROOT_KEY_2.keyinfo
  NOTARY_1=`lotus wallet new secp256k1`
  NOTARY_2=`lotus wallet new secp256k1`
  echo $NOTARY_1 > $CURIO_MK12_CLIENT_REPO/notary_1
  echo $NOTARY_2 > $CURIO_MK12_CLIENT_REPO/notary_2
  echo Notary 1: $NOTARY_1
  echo Notary 2: $NOTARY_2

  echo Add verifier root_key_1 notary_1
  lotus-shed verifreg add-verifier $ROOT_KEY_1 $NOTARY_1 1000000000000
  sleep 15
  echo Msig inspect t080
  lotus msig inspect t080
  PARAMS=`lotus msig inspect t080 | tail -1 | awk '{print $8}'`
  echo Params: $PARAMS
  echo Msig approve
  lotus msig approve --from=$ROOT_KEY_2 t080 0 t0100 t06 0 2 $PARAMS

  echo Send 10 FIL to NOTARY_1
  lotus send $NOTARY_1 10
  touch $CURIO_MK12_CLIENT_REPO/.init.filplus
  sleep 10
fi

## Grant datacap to client
if [ ! -f $CURIO_MK12_CLIENT_REPO/.init.datacap ]; then
  notary=`lotus filplus list-notaries | awk '$2 != 0 {print $1}' | awk -F':' '{print $1}'`
  def_wallet=$(sptool --actor t01000 toolbox mk12-client wallet default)
  lotus filplus grant-datacap --from $notary $def_wallet 1000000000
  touch $CURIO_MK12_CLIENT_REPO/.init.datacap
fi

# Ensure the scanning directory exists
SCAN_DIR="/var/lib/curio-client/data"
if [ ! -d "$SCAN_DIR" ]; then
    echo "Creating scan directory: $SCAN_DIR"
    mkdir -p "$SCAN_DIR"
    mkdir -p /var/lib/curio-client/public
fi

# Start piece-server in HTTP mode (no HTTPS)
echo "Starting piece-server on localhost:12320 scanning $SCAN_DIR"
piece-server run --dir="$SCAN_DIR" --port=12320 --bind="$myip"
