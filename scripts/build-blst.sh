#!/usr/bin/env bash

if [ ! -d "extern/supra_seal/deps/blst" ]; then
    git clone https://github.com/supranational/blst.git extern/supra_seal/deps/blst
    (cd extern/supra_seal/deps/blst
     ./build.sh -march=native)
fi
