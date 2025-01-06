#!/usr/bin/env bash

if [ ! -d "extern/supraseal/deps/blst" ]; then
    git clone https://github.com/supranational/blst.git extern/supraseal/deps/blst
    (cd extern/supraseal/deps/blst
     ./build.sh -march=native)
fi
