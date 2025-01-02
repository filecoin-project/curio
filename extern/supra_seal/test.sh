#!/bin/bash

# Copyright Supranational LLC

set -e
set -x

# Cache files, assumed to be CC for the tests below. To generate, edit
# fil-proofs-tooling/src/bin/benchy/window_post.rs in rust-fil-proofs. Around
# line 129, change
#                 .map(|_| rand::random::<u8>())
# to
#                 .map(|_| 0x0)
#
# env RUST_LOG=trace FIL_PROOFS_USE_MULTICORE_SDR=1 FIL_PROOFS_USE_GPU_COLUMN_BUILDER=1 FIL_PROOFS_USE_GPU_TREE_BUILDER=1 BELLMAN_CUSTOM_GPU="NVIDIA GeForce RTX 3090:10496" ./target/release/benchy window-post --size 512MiB --cache ./cache_benchy_run_512MiB 2>&1 | tee run_512m_0.log
# env RUST_LOG=trace FIL_PROOFS_USE_MULTICORE_SDR=1 FIL_PROOFS_USE_GPU_COLUMN_BUILDER=1 FIL_PROOFS_USE_GPU_TREE_BUILDER=1 BELLMAN_CUSTOM_GPU="NVIDIA GeForce RTX 3090:10496" ./target/release/benchy window-post --size 32GiB --cache ./cache_benchy_run_32GiB 2>&1 | tee run_32g_0.log

cache_16KiB="../cache_benchy_run_16KiB"
cache_512MiB="../cache_benchy_run_512MiB"
cache_32GiB="../cache_benchy_run_32GiB"

# This cache is random data, not cc
cache_512MiB_rand="../cache_benchy_run_512MiB_rand"

test_dir="/var/tmp/supra_seal/test_tmp"

rm -fr $test_dir/*

./build.sh -r

echo "************************************************************"
echo "* 16KiB pc2"
echo "************************************************************"

mkdir -p $test_dir/pc2_test_16KiB
./bin/pc2 -i $cache_16KiB -o $test_dir/pc2_test_16KiB -b 16KiB
cmp $cache_16KiB/p_aux $test_dir/pc2_test_16KiB/p_aux
cmp $cache_16KiB/sc-02-data-tree-c-0.dat $test_dir/pc2_test_16KiB/sc-02-data-tree-c-0.dat
cmp $cache_16KiB/sc-02-data-tree-c-7.dat $test_dir/pc2_test_16KiB/sc-02-data-tree-c-7.dat
cmp $cache_16KiB/sc-02-data-tree-r-last-0.dat $test_dir/pc2_test_16KiB/sc-02-data-tree-r-last-0.dat
cmp $cache_16KiB/sc-02-data-tree-r-last-1.dat $test_dir/pc2_test_16KiB/sc-02-data-tree-r-last-1.dat

echo "************************************************************"
echo "* 512MiB pc2"
echo "************************************************************"

mkdir -p $test_dir/pc2_test_512MiB
./bin/pc2 -i $cache_512MiB -o $test_dir/pc2_test_512MiB -b 512MiB
cmp $cache_512MiB/p_aux $test_dir/pc2_test_512MiB/p_aux
cmp $cache_512MiB/sc-02-data-tree-c.dat $test_dir/pc2_test_512MiB/sc-02-data-tree-c.dat
cmp $cache_512MiB/sc-02-data-tree-r-last.dat $test_dir/pc2_test_512MiB/sc-02-data-tree-r-last.dat
cmp $cache_512MiB/sealed-file $test_dir/pc2_test_512MiB/sealed-file

echo "************************************************************"
echo "* 512MiB tree-r random"
echo "************************************************************"

mkdir -p $test_dir/tree-r_test_512MiB_rand
./bin/tree_r -l $cache_512MiB_rand/sc-02-data-layer-2.dat -d $cache_512MiB_rand/staged-file -o $test_dir/tree-r_test_512MiB_rand -b 512MiB
# Only the root of r is written to p_aux
cmp -i 32 ../cache_benchy_run_512MiB_rand/p_aux /var/tmp/supra_seal/test_tmp/tree-r_test_512MiB_rand/p_aux
cmp $cache_512MiB_rand/sc-02-data-tree-r-last.dat $test_dir/tree-r_test_512MiB_rand/sc-02-data-tree-r-last.dat
cmp $cache_512MiB_rand/sealed-file $test_dir/tree-r_test_512MiB_rand/sealed-file

echo "************************************************************"
echo "* 512MiB tree-r-cpu random"
echo "************************************************************"

mkdir -p $test_dir/tree-r-cpu_test_512MiB_rand
./bin/tree_r_cpu -l $cache_512MiB_rand/sc-02-data-layer-2.dat -d $cache_512MiB_rand/staged-file -o $test_dir/tree-r-cpu_test_512MiB_rand -b 512MiB
cmp $cache_512MiB_rand/sc-02-data-tree-r-last.dat $test_dir/tree-r-cpu_test_512MiB_rand/sc-02-data-tree-r-last.dat
cmp $cache_512MiB_rand/sealed-file $test_dir/tree-r-cpu_test_512MiB_rand/sealed-file


echo "************************************************************"
echo "* 32GiB pc2"
echo "************************************************************"

mkdir -p $test_dir/pc2_test_32GiB
./bin/pc2 -i $cache_32GiB -o $test_dir/pc2_test_32GiB -b 32GiB
cmp $cache_32GiB/p_aux $test_dir/pc2_test_32GiB/p_aux
cmp $cache_32GiB/sc-02-data-tree-c-0.dat $test_dir/pc2_test_32GiB/sc-02-data-tree-c-0.dat
cmp $cache_32GiB/sc-02-data-tree-c-7.dat $test_dir/pc2_test_32GiB/sc-02-data-tree-c-7.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-0.dat $test_dir/pc2_test_32GiB/sc-02-data-tree-r-last-0.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-1.dat $test_dir/pc2_test_32GiB/sc-02-data-tree-r-last-1.dat
cmp $cache_32GiB/sealed-file $test_dir/pc2_test_32GiB/sealed-file

echo "************************************************************"
echo "* 32GiB tree-r CC"
echo "************************************************************"

mkdir -p $test_dir/tree-r_test_32GiB
./bin/tree_r -l $cache_32GiB/sc-02-data-layer-11.dat -d $cache_32GiB/staged-file -o $test_dir/tree-r_test_32GiB -b 32GiB
# Only the root of r is written to p_aux
cmp -i 32 ../cache_benchy_run_32GiB/p_aux /var/tmp/supra_seal/test_tmp/tree-r_test_32GiB/p_aux
cmp $cache_32GiB/sc-02-data-tree-r-last-0.dat $test_dir/tree-r_test_32GiB/sc-02-data-tree-r-last-0.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-1.dat $test_dir/tree-r_test_32GiB/sc-02-data-tree-r-last-1.dat

echo "************************************************************"
echo "* 32GiB tree-r-cpu"
echo "************************************************************"

mkdir -p $test_dir/tree-r-cpu_test_32GiB
./bin/tree_r_cpu -l $cache_32GiB/sc-02-data-layer-11.dat -d $cache_32GiB/staged-file -o $test_dir/tree-r-cpu_test_32GiB -b 32GiB
cmp $cache_32GiB/sc-02-data-tree-r-last-0.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-0.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-1.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-1.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-2.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-2.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-3.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-3.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-4.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-4.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-5.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-5.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-6.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-6.dat
cmp $cache_32GiB/sc-02-data-tree-r-last-7.dat $test_dir/tree-r-cpu_test_32GiB/sc-02-data-tree-r-last-7.dat

echo "************************************************************"
echo "* 32GiB c2"
echo "************************************************************"

cd demos/c2-test
cargo test --release --test c2 -- --nocapture

# echo "************************************************************"
# echo "* 512MiB sealing pipeline"
# echo "************************************************************"

# ./exec.sh 512MiB

