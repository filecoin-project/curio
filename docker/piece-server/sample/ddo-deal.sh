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

mv /var/lib/curio-client/data/$PAYLOAD_CID.car /var/lib/curio-client/data/$COMMP_CID

sptool --actor t01000 toolbox mk12-client allocate -y -p $miner_actor --piece-cid $COMMP_CID --piece-size $PIECE --confidence 0

CLIENT=$(sptool --actor t01000 toolbox mk12-client wallet default)

ALLOC=$(sptool --actor t01000 toolbox mk12-client list-allocations -j | jq -r --arg cid "$COMMP_CID" '.allocations | to_entries[] | select(.value.Data["/"] == $cid) | .key')

printf "${ci}Making a DDO deal with provider $miner_actor \
PAYLOAD_CID $PAYLOAD_CID COMMP $COMMP_CID car-size $CAR piece-size $PIECE \
CLIENT $CLIENT Allocation $ALLOC\n\n${cn}"

curio --db-host yugabyte market ddo --actor $miner_actor $CLIENT $ALLOC