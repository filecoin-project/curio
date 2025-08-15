#!/usr/bin/env bash
set -e

ci="\e[3m"
cn="\e[0m"


put="${1:-false}"
offline="${2:-false}"
chunks="${3:-51200}"
links="${4:-100}"

printf "${ci}sptool --actor t01000 toolbox mk12-client generate-rand-car -c=$chunks -l=$links -s=5120000 /var/lib/curio-client/data/ | awk '{print $NF}'\n\n${cn}"
FILE=`sptool --actor t01000 toolbox mk12-client generate-rand-car -c=$chunks -l=$links -s=5120000 /var/lib/curio-client/data/ | awk '{print $NF}'`
read COMMP_CID PIECE CAR < <(sptool --actor t01000 toolbox mk20-client commp $FILE 2>/dev/null | awk -F': ' '/CID/ {cid=$2} /Piece/ {piece=$2} /Car/ {car=$2} END {print cid, piece, car}')

mv $FILE /var/lib/curio-client/data/$COMMP_CID

miner_actor=$(lotus state list-miners | grep -v t01000)

if [ "$put" == "true" ]; then
  ###################################################################################
    printf "${ci}sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor \
    --pcidv2=$COMMP_CID \
    --contract-address 0xtest --contract-verify-method test --put\n\n${cn}"

    sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor --pcidv2=$COMMP_CID --contract-address 0xtest --contract-verify-method test --put

else

  if [ "$offline" == "true" ]; then

    ###################################################################################
    printf "${ci}sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor \
    --pcidv2=$COMMP_CID \
    --contract-address 0xtest --contract-verify-method test\n\n${cn}"

    sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor --pcidv2=$COMMP_CID  --contract-address 0xtest --contract-verify-method test

  else
    ###################################################################################
    printf "${ci}sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor \
    --http-url=http://piece-server:12320/pieces?id=$COMMP_CID \
    --pcidv2=$COMMP_CID \
    --contract-address 0xtest --contract-verify-method test\n\n${cn}"

    sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor --http-url=http://piece-server:12320/pieces?id=$COMMP_CID --pcidv2=$COMMP_CID --contract-address 0xtest --contract-verify-method test

  fi

fi


