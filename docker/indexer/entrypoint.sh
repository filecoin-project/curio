#!/usr/bin/env bash
set -e
STORETHEINDEX_PATH=$STORETHEINDEX_PATH
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

# Init Indexer
 if [ ! -f $STORETHEINDEX_PATH/.init ]; then
  echo "Initialising indexer"
  storetheindex init --listen-admin /ip4/0.0.0.0/tcp/3002 --listen-finder /ip4/0.0.0.0/tcp/3000 --listen-ingest /ip4/0.0.0.0/tcp/3001 --cachesize 10485760 --no-bootstrap
  touch $STORETHEINDEX_PATH/.init
fi

# Start piece-server in HTTP mode (no HTTPS)
echo "Starting indexer"
storetheindex daemon
