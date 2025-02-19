#!/usr/bin/env bash
set -e
CURIO_MK12_CLIENT_REPO=$CURIO_MK12_CLIENT_REPO
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
