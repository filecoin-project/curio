#!/usr/bin/env bash
###################################################################################
# sample demo script for making a deal with curio mk12 client
###################################################################################
set -e
# colors
cb="\e[1m"
ci="\e[3m" 
cn="\e[0m"
printf "5. Let's generate a sample file in ${ci}/var/lib/curio-client/public/sample.txt${cn}. We will use it as a demo file.\n\n"
read -rsp $'Press any key to generate it...\n\n' -n1 key
rm -f /var/lib/curio-client/public/sample.txt
for i in {1..57}; do echo "Hi Curio, $i times" >> /var/lib/curio-client/public/sample.txt; done

printf "\n\nFile content:\n\n"
cat /var/lib/curio-client/public/sample.txt
printf "\n\n \
###################################################################################\n"
###################################################################################

printf "6. After that, you need to generate a car file for data you want to store on Filecoin (${ci}/var/lib/curio-client/public/sample.txt${cn}), \
and note down its ${ci}payload-cid${cn}. \
We will use the ${ci}car${cn} utility\n \
 : ${ci}car c -f /var/lib/curio-client/data/sample.car --version 1 /var/lib/curio-client/public/sample.txt${cn}\n\n"
read -rsp $'Press any key to execute it...\n\n' -n1 key
rm -rf /var/lib/curio-client/data/sample.car
car c -f /var/lib/curio-client/data/sample.car --version 1 /var/lib/curio-client/public/sample.txt

PAYLOAD_CID=`car root /var/lib/curio-client/data/sample.car`
printf "\n\nDone. We noted payload-cid = ${ci}$PAYLOAD_CID${cn}\n \
###################################################################################\n"
###################################################################################
printf "7. Then you need to calculate the commp and piece size for the generated car file:\n \
 : ${ci}sptool --actor t01000 toolbox mk12-client commp /var/lib/curio-client/data/sample.car${cn}\n\n"
read -rsp $'Press any key to execute it...\n\n' -n1 key

read COMMP_CID PIECE CAR < <(sptool --actor t01000 toolbox mk12-client commp /var/lib/curio-client/data/sample.car 2>/dev/null | awk -F': ' '/CID/ {cid=$2} /Piece/ {piece=$2} /Car/ {car=$2} END {print cid, piece, car}')
cp /var/lib/curio-client/data/sample.car /var/lib/curio-client/data/$COMMP_CID.car

printf "\n\nYes. We also have remembered these values:\n \
Commp-cid = $COMMP_CID \n \
Piece size = $PIECE \n \
Car size = $CAR \n \
###################################################################################\n"
###################################################################################
miner_actor=$(lotus state list-miners | grep -v t01000)
printf "8. That's it. We are ready to make the deal. \n \
 : ${ci}sptool --actor t01000 toolbox mk12-client deal --verified=false --provider=$miner_actor \
--http-url=http://demo-http-server/sample.car \
--commp=$COMMP_CID --car-size=$CAR --piece-size=$PIECE \
--payload-cid=$PAYLOAD_CID --storage-price 20000000000\n\n${cn}"
read -rsp $'Press any key to make the deal...\n\n' -n1 key


until sptool --actor t01000 toolbox mk12-client deal --verified=false \
           --provider=$miner_actor \
           --http-url=http://piece-server:12320/pieces?id=$COMMP_CID \
           --commp=$COMMP_CID \
           --car-size=$CAR \
           --piece-size=$PIECE \
           --payload-cid=$PAYLOAD_CID --storage-price 20000000000
do  
    printf "\nThe error has occured.\n\n"
    read -rsp $'\n\nPress any key to try making the deal again...\n' -n1 key 
done           

printf "\n\n   ${cb}Congrats! You have made it.${cn}\n\n \
###################################################################################\n"
####################################################################################
#printf "10. To retrieve the file from the ${cb}storage provider${cn} system you can use \n\
#${ci}boost retrieve${cn} command.\n\
#: ${ci}boost retrieve -p t01000 -o /app/output $PAYLOAD_CID ${cn}\n\n"
#
#read -rsp $'Press any key to show the file content...\n\n' -n1 key
#until boost retrieve -p t01000 -o /app/output $PAYLOAD_CID
#cat /app/output/sample.txt
#do
#    printf "\nFile publishing may take time, please wait some time until the deal is finished and try again.\n\n"
#    read -rsp $'Press any key to try again...\n' -n1 key
#done
#
#printf "\n\nIf you see a file content you have just completed the demo. You have succesfully:\n\n\
#  1) initiated the boost client\n\
#  2) prepared sample file\n\
#  3) sent the sample file to the Filecoin devnet\n\
#  4) retrieved the content of the file from it.\n\n\
#More info at ${cb}https://boost.filecoin.io${cn} or ${cb}https://github.com/filecoin-project/boost${cn}.\n\n\n"
