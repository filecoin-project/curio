#!/usr/bin/env bash
set -e
echo CURIO_REPO_PATH=$CURIO_REPO_PATH
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

echo All ready. Lets go
myip=`nslookup curio | grep -v "#" | grep Address | awk '{print $2}'`

if [ ! -f $CURIO_REPO_PATH/.init.curio ]; then
  echo Wait for lotus-miner is ready ...
  lotus-miner wait-api

  if [ ! -f $CURIO_REPO_PATH/.init.setup ]; then
  export DEFAULT_WALLET=`lotus wallet default`
	echo Create a new miner actor ...
	sptool --actor t01000 actor new-miner --owner $DEFAULT_WALLET --worker $DEFAULT_WALLET --send $DEFAULT_WALLET 8MiB
	touch $CURIO_REPO_PATH/.init.setup
	fi

	if [ ! -f $CURIO_REPO_PATH/.init.config ]; then

	newminer=`lotus state list-miners | grep -v t01000`
	echo "New Miner is $newminer"
	echo Initiating a new Curio cluster ...
	curio config new-cluster $newminer
	echo Creating market config...
  curio config get base | sed -e 's/#Miners = \[\]/Miners = ["'"$newminer"'"]/g' | curio config set --title base
  CONFIG_CONTENT='[HTTP]
    DelegateTLS = true
    DomainName = "curio"
    Enable = true

  [Ingest]
    MaxDealWaitTime = "0h0m30s"

  [Market]
    [Market.StorageMarketConfig]
      [Market.StorageMarketConfig.IPNI]
        DirectAnnounceURLs = ["http://indexer:3001"]
        ServiceURL = ["http://indexer:3000"]
      [Market.StorageMarketConfig.MK12]
          ExpectedPoRepSealDuration = "0h1m0s"
          ExpectedSnapSealDuration = "0h1m0s"
          PublishMsgPeriod = "0h0m10s"

      [[Market.StorageMarketConfig.PieceLocator]]
        URL = "http://piece-server:12320/pieces"

  [Subsystems]
    EnableCommP = true
    EnableDealMarket = true
    EnableParkPiece = true

  [Batching]
    [Batching.PreCommit]
      Timeout = "0h0m5s"
      Slack = "6h0m0s"
    [Batching.Commit]
      Timeout = "0h0m5s"
      Slack = "1h0m0s"
  '
  echo "$CONFIG_CONTENT" | curio config create --title market
  touch $CURIO_REPO_PATH/.init.config
  fi

  echo Starting Curio node to attach storage ...
  curio run --nosync --layers seal,post,market,gui &
  CURIO_PID=`echo $!`
  until curio cli --machine $myip:12300 wait-api; do
    echo "Waiting for the curio CLI to become ready..."
    sleep 5
  done
  curio cli --machine $myip:12300 storage attach --init --seal --store $CURIO_REPO_PATH
  touch $CURIO_REPO_PATH/.init.curio
  echo Stopping Curio node ...
  echo Try to stop curio...
      kill -15 $CURIO_PID || kill -9 $CURIO_PID
	echo Done
fi

echo Starting curio node ...
exec curio run --nosync --name devnet --layers seal,post,market,gui

