#!/usr/bin/env bash
set -e

# ANSI escape codes for styling
ci="\e[3m"
cn="\e[0m"

# Parameters for file generation
chunks=512
links=8
output_dir="/var/lib/curio-client/data/"
size=99700
num_files=63
piece_size=$((8 * 1024 * 1024))  # 8 MiB

# Array to store generated CAR files
declare -a car_files

# Step 1: Generate all files
echo "Generating $num_files random CAR files (size: $size bytes):"
for i in $(seq 1 "$num_files"); do
    echo "Generating file $i..."
    output=$(sptool --actor t01000 toolbox mk12-client generate-rand-car -c=$chunks -l=$links -s=$size "$output_dir" 2>&1)
    car_file=$(echo "$output" | awk '{print $NF}')
    new_car_file="${car_file%.car}"
    mv "$car_file" "$new_car_file"
    car_file="$new_car_file"

    if [[ -n "$car_file" ]]; then
        car_files+=("$car_file")
        echo "File $i generated: $car_file"
    else
        echo "Error: Failed to generate file $i" >&2
        exit 1
    fi
done

if [[ ${#car_files[@]} -eq 0 ]]; then
    echo "Error: No files were generated. Exiting." >&2
    exit 1
fi


# Declare the base command and arguments
base_command="sptool --actor t01000 toolbox mk20-client aggregate --piece-size=$piece_size"

# Append --file arguments for each file in the car_files array
for car_file in "${car_files[@]}"; do
    base_command+=" --files=$car_file"
done

# Debugging: Print the full constructed command
printf "${ci}%s\n\n${cn}" "$base_command"

# Execute the constructed command
aggregate_output=$($base_command 2>&1)

echo "$aggregate_output"

# Step 3: Extract `CommP CID` and `Piece size` from the aggregate output
commp_cid=$(echo "$aggregate_output" | awk -F': ' '/CommP CID/ {print $2}' | xargs)

# Validate that we got proper output
if [[ -z "$commp_cid" ]]; then
    echo "Error: Failed to extract CommP CID from aggregation output" >&2
    exit 1
fi

# Step 4: Check and display the aggregate file
aggregate_file="aggregate_${commp_cid}"
if [[ -f "$aggregate_file" ]]; then
    echo "Aggregate file stored at: $aggregate_file"
    echo "Content of $aggregate_file:"
    cat "$aggregate_file"
else
    echo "Error: Aggregate file $aggregate_file not found!" >&2
fi

# Step 5: Print Results
echo -e "\n${ci}Aggregation Results:${cn}"
echo "CommP CID: $commp_cid"


miner_actor=$(lotus state list-miners | grep -v t01000)

###################################################################################
printf "${ci}sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor \
--pcidv2=$commp_cid --contract-address 0xtest --contract-verify-method test \
--aggregate "$aggregate_file"\n\n${cn}"

sptool --actor t01000 toolbox mk20-client deal --provider=$miner_actor --pcidv2=$commp_cid --contract-address 0xtest --contract-verify-method test --aggregate "$aggregate_file"

echo -e "\nDone!"