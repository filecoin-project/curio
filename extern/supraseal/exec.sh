#!/bin/bash -e

# Copyright Supranational LLC

set -x

RUNTIME="" # Compile for all sector sizes
SECTOR_SIZE=""
while getopts 'b:' flag
do
    case "${flag}" in
        b) SECTOR_SIZE="${OPTARG}";;
    esac
done

if [[ -z $SECTOR_SIZE ]]; then
    echo "Please specify a sector size. e.g exec.sh -b 32GiB"
    exit 1
fi

if [[ "$SECTOR_SIZE" != "32GiB" && "$SECTOR_SIZE" != "512MiB" ]]; then
     RUNTIME="-r"
fi

./build.sh $RUNTIME

cd demos/rust
#touch build.rs
env RUSTFLAGS="-C target-cpu=native" \
    cargo +nightly build --release
sudo ./target/release/supra-seal-demo $SECTOR_SIZE

cd ../..
