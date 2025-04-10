#!/usr/bin/env bash
set -e

ci="\e[3m"
cn="\e[0m"

chunks="${1:-51200}"
links="${2:-100}"

printf "${ci}sptool --actor t01000 toolbox mk12-client generate-rand-car -c=$chunks -l=$links -s=5120000 /var/lib/curio-client/data/ | awk '{print $NF}'\n\n${cn}"

FILE=`sptool --actor t01000 toolbox mk12-client generate-rand-car -c=$chunks -l=$links -s=5120000 /var/lib/curio-client/data/ | awk '{print $NF}'`
PAYLOAD_CID=$(find "$FILE" | xargs -I{} basename {} | sed 's/\.car//')

read COMMP_CID PIECE CAR < <(sptool --actor t01000 toolbox mk12-client commp $FILE 2>/dev/null | awk -F': ' '/CID/ {cid=$2} /Piece/ {piece=$2} /Car/ {car=$2} END {print cid, piece, car}')
miner_actor=$(lotus state list-miners | grep -v t01000)

###################################################################################
printf "${ci}sptool --actor t01000 toolbox mk12-client deal --http --provider=$miner_actor \
--http-url=http://piece-server:12320/pieces?id=$PAYLOAD_CID \
--commp=$COMMP_CID --car-size=$CAR --piece-size=$PIECE \
--payload-cid=$PAYLOAD_CID --storage-price 20000000000\n\n${cn}"

sptool --actor t01000 toolbox mk12-client deal --http --provider=$miner_actor --http-url=http://piece-server:12320/pieces?id=$PAYLOAD_CID --commp=$COMMP_CID --car-size=$CAR --piece-size=$PIECE --payload-cid=$PAYLOAD_CID --storage-price 20000000000