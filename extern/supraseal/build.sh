#!/bin/bash

# Copyright Supranational LLC

set -e
set -x

SECTOR_SIZE="" # Compile for all sector sizes
while getopts r flag
do
    case "${flag}" in
        r) SECTOR_SIZE="-DRUNTIME_SECTOR_SIZE";;
    esac
done

# Function to check GCC version - enforces GCC 12 for compatibility
check_gcc_version() {
    local gcc_version=$(gcc -dumpversion | cut -d. -f1)
    local target_gcc_version=12
    
    # Check if default GCC is version 12
    if [ "$gcc_version" -eq "$target_gcc_version" ]; then
        echo "Using GCC $gcc_version"
        return 0
    fi
    
    # If not GCC 12, try to find and use gcc-12
    if command -v gcc-12 &> /dev/null && command -v g++-12 &> /dev/null; then
        echo "Setting CC, CXX, and NVCC_PREPEND_FLAGS to use GCC 12 for compatibility."
        export CC=gcc-12
        export CXX=g++-12
        export NVCC_PREPEND_FLAGS="-ccbin /usr/bin/g++-12"
        return 0
    fi
    
    # GCC 12 not found
    echo "Error: GCC 12 is required but not found."
    echo "Current GCC version: $gcc_version"
    echo "Please install GCC 12:"
    echo "  On Ubuntu/Debian: sudo apt-get install gcc-12 g++-12"
    echo "  On Fedora: sudo dnf install gcc-12 gcc-c++-12"
    exit 1
}

# Call the function to check GCC version
check_gcc_version

set -x

CC=${CC:-cc}
CXX=${CXX:-c++}
NVCC=${NVCC:-nvcc}

# Create and activate Python virtual environment
# This avoids needing PIP_BREAK_SYSTEM_PACKAGES on Ubuntu 24.04+
VENV_DIR="$(pwd)/.venv"
if [ ! -d "$VENV_DIR" ] || [ ! -f "$VENV_DIR/bin/activate" ]; then
    echo "Creating Python virtual environment..."
    if ! python3 -m venv "$VENV_DIR"; then
        echo "Error: python3-venv is required but not available."
        echo "Please install it:"
        echo "  On Ubuntu/Debian: sudo apt-get install python3-venv"
        echo "  On Fedora: sudo dnf install python3-virtualenv"
        echo ""
        echo "Or if you prefer, you can install dependencies manually:"
        echo "  pip3 install --user meson ninja pyelftools"
        exit 1
    fi
fi

# Activate the virtual environment
if [ -f "$VENV_DIR/bin/activate" ]; then
    source "$VENV_DIR/bin/activate"
else
    echo "Error: Virtual environment activation script not found at $VENV_DIR/bin/activate"
    exit 1
fi

# Install Python build tools in the virtual environment
echo "Installing Python build tools in virtual environment..."
pip install --upgrade pip
pip install meson ninja pyelftools

# Ensure venv is in PATH for subprocesses
export PATH="$VENV_DIR/bin:$PATH"

# Detect CUDA installation path - search for CUDA 12+ (required for modern architectures)
CUDA=""
MIN_CUDA_VERSION=12

# Try common CUDA installation paths
for cuda_path in /usr/local/cuda-13.0 /usr/local/cuda-13 /usr/local/cuda-12.6 /usr/local/cuda-12 /usr/local/cuda /opt/cuda; do
    if [ -d "$cuda_path" ] && [ -f "$cuda_path/bin/nvcc" ]; then
        # Check CUDA version
        CUDA_VER_CHECK=$($cuda_path/bin/nvcc --version | grep "release" | sed -n 's/.*release \([0-9]*\)\.\([0-9]*\).*/\1/p')
        if [ "$CUDA_VER_CHECK" -ge "$MIN_CUDA_VERSION" ] 2>/dev/null; then
            CUDA=$cuda_path
            NVCC=$cuda_path/bin/nvcc
            CUDA_VERSION=$CUDA_VER_CHECK
            break
        fi
    fi
done

# If not found in standard paths, check if nvcc in PATH is CUDA 12+
if [ -z "$CUDA" ] && command -v nvcc &> /dev/null; then
    CUDA_VER_CHECK=$(nvcc --version | grep "release" | sed -n 's/.*release \([0-9]*\)\.\([0-9]*\).*/\1/p')
    if [ "$CUDA_VER_CHECK" -ge "$MIN_CUDA_VERSION" ] 2>/dev/null; then
        CUDA=$(dirname $(dirname $(which nvcc)))
        NVCC=nvcc
        CUDA_VERSION=$CUDA_VER_CHECK
    fi
fi

if [ -z "$CUDA" ]; then
    echo "Error: CUDA $MIN_CUDA_VERSION or newer not found."
    echo "Please install CUDA Toolkit (version 12.0 or later):"
    echo "  Download from: https://developer.nvidia.com/cuda-downloads"
    echo ""
    echo "Checked locations:"
    echo "  - /usr/local/cuda-13.0"
    echo "  - /usr/local/cuda-13"
    echo "  - /usr/local/cuda-12.6"
    echo "  - /usr/local/cuda-12"
    echo "  - /usr/local/cuda"
    echo "  - PATH (found: $(which nvcc 2>/dev/null || echo 'not found'))"
    if command -v nvcc &> /dev/null; then
        echo ""
        echo "Note: Found nvcc in PATH, but it's version $(nvcc --version | grep release | sed -n 's/.*release \([0-9.]*\).*/\1/p'), need $MIN_CUDA_VERSION.x or newer"
    fi
    exit 1
fi

# Ensure CUDA bin directory is in PATH
export PATH=$CUDA/bin:$PATH

echo "Found CUDA $CUDA_VERSION at: $CUDA"
SPDK="deps/spdk-v24.05"
# CUDA 13 architectures - removed compute_70 (Volta) as it's no longer supported in CUDA 13+
# sm_80: Ampere (A100), sm_86: Ampere (RTX 30xx), sm_89: Ada Lovelace (RTX 40xx, L40), sm_90: Hopper (H100)
CUDA_ARCH="-arch=sm_80 -gencode arch=compute_80,code=sm_80 -gencode arch=compute_86,code=sm_86 -gencode arch=compute_89,code=sm_89 -gencode arch=compute_90,code=sm_90 -t0"
CXXSTD=`$CXX -dM -E -x c++ /dev/null | \
        awk '{ if($2=="__cplusplus" && $3<"2017") print "-std=c++17"; }'`

INCLUDE="-I$SPDK/include -I$SPDK/isa-l/.. -I$SPDK/dpdk/build/include"
CFLAGS="$SECTOR_SIZE $INCLUDE -g -O2"
CXXFLAGS="$CFLAGS -march=native $CXXSTD \
          -fPIC -fno-omit-frame-pointer -fno-strict-aliasing \
          -fstack-protector -fno-common \
          -D_GNU_SOURCE -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=2 \
          -DSPDK_GIT_COMMIT=4be6d3043 -pthread \
          -Wall -Wextra -Wno-unused-variable -Wno-unused-parameter -Wno-missing-field-initializers \
          -Wformat -Wformat-security"

LDFLAGS="-fno-omit-frame-pointer -Wl,-z,relro,-z,now -Wl,-z,noexecstack -fuse-ld=bfd\
         -L$SPDK/build/lib \
         -Wl,--whole-archive -Wl,--no-as-needed \
         -lspdk_log \
         -lspdk_bdev_malloc \
         -lspdk_bdev_null \
         -lspdk_bdev_nvme \
         -lspdk_bdev_passthru \
         -lspdk_bdev_lvol \
         -lspdk_bdev_raid \
         -lspdk_bdev_error \
         -lspdk_bdev_gpt \
         -lspdk_bdev_split \
         -lspdk_bdev_delay \
         -lspdk_bdev_zone_block \
         -lspdk_blobfs_bdev \
         -lspdk_blobfs \
         -lspdk_blob_bdev \
         -lspdk_lvol \
         -lspdk_blob \
         -lspdk_nvme \
         -lspdk_bdev_ftl \
         -lspdk_ftl \
         -lspdk_bdev_aio \
         -lspdk_bdev_virtio \
         -lspdk_virtio \
         -lspdk_vfio_user \
         -lspdk_accel_ioat \
         -lspdk_ioat \
         -lspdk_scheduler_dynamic \
         -lspdk_env_dpdk \
         -lspdk_scheduler_dpdk_governor \
         -lspdk_scheduler_gscheduler \
         -lspdk_sock_posix \
         -lspdk_event \
         -lspdk_event_bdev \
         -lspdk_bdev \
         -lspdk_notify \
         -lspdk_dma \
         -lspdk_event_accel \
         -lspdk_accel \
         -lspdk_event_vmd \
         -lspdk_vmd \
         -lspdk_event_sock \
         -lspdk_init \
         -lspdk_thread \
         -lspdk_trace \
         -lspdk_sock \
         -lspdk_rpc \
         -lspdk_jsonrpc \
         -lspdk_json \
         -lspdk_util \
         -lspdk_keyring \
         -lspdk_keyring_file \
         -lspdk_keyring_linux \
         -lspdk_event_keyring \
         -Wl,--no-whole-archive $SPDK/build/lib/libspdk_env_dpdk.a \
         -Wl,--whole-archive $SPDK/dpdk/build/lib/librte_bus_pci.a \
         $SPDK/dpdk/build/lib/librte_cryptodev.a \
         $SPDK/dpdk/build/lib/librte_dmadev.a \
         $SPDK/dpdk/build/lib/librte_eal.a \
         $SPDK/dpdk/build/lib/librte_ethdev.a \
         $SPDK/dpdk/build/lib/librte_hash.a \
         $SPDK/dpdk/build/lib/librte_kvargs.a \
         $SPDK/dpdk/build/lib/librte_log.a \
         $SPDK/dpdk/build/lib/librte_mbuf.a \
         $SPDK/dpdk/build/lib/librte_mempool.a \
         $SPDK/dpdk/build/lib/librte_mempool_ring.a \
         $SPDK/dpdk/build/lib/librte_net.a \
         $SPDK/dpdk/build/lib/librte_pci.a \
         $SPDK/dpdk/build/lib/librte_power.a \
         $SPDK/dpdk/build/lib/librte_rcu.a \
         $SPDK/dpdk/build/lib/librte_ring.a \
         $SPDK/dpdk/build/lib/librte_telemetry.a \
         $SPDK/dpdk/build/lib/librte_vhost.a \
         -Wl,--no-whole-archive \
         -lnuma -ldl \
         -L$SPDK/isa-l/.libs -L$SPDK/isa-l-crypto/.libs -lisal -lisal_crypto \
         -pthread -lrt -luuid -lssl -lcrypto -lm -laio -lfuse3 -larchive -lkeyutils"

# Check for the default result directory
# if [ ! -d "/var/tmp/supraseal" ]; then
#    mkdir -p /var/tmp/supraseal
# fi

rm -fr obj
mkdir -p obj

rm -fr bin
mkdir -p bin

mkdir -p deps
if [ ! -d $SPDK ]; then
    git clone --branch v24.05 https://github.com/spdk/spdk --recursive $SPDK
    (cd $SPDK
     # Use the virtual environment for Python packages
     # Ensure venv is active and in PATH for Python package installation
     export VIRTUAL_ENV="$VENV_DIR"
     export PATH="$VENV_DIR/bin:$PATH"
     export PIP="$VENV_DIR/bin/pip"
     export PYTHON="$VENV_DIR/bin/python"
     # Run pkgdep.sh without sudo - system packages should already be installed
     # Python packages will be installed in the venv automatically
     # If system packages are missing, pkgdep.sh will fail gracefully
     env VIRTUAL_ENV="$VENV_DIR" PATH="$VENV_DIR/bin:$PATH" PIP="$VENV_DIR/bin/pip" PYTHON="$VENV_DIR/bin/python" scripts/pkgdep.sh || {
         echo "Warning: pkgdep.sh failed (likely system packages already installed). Continuing..."
     }
     ./configure --with-virtio --with-vhost \
                 --without-fuse --without-crypto \
                 --disable-unit-tests --disable-tests \
                 --disable-examples --disable-apps \
                 --without-fio --without-xnvme --without-vbdev-compress \
                 --without-rbd --without-rdma --without-iscsi-initiator \
                 --without-ocf --without-uring
     make -j$(nproc))
fi
if [ ! -d "deps/sppark" ]; then
    git clone --branch v0.1.10 https://github.com/supranational/sppark.git deps/sppark
fi
if [ ! -d "deps/blst" ]; then
    git clone https://github.com/supranational/blst.git deps/blst
    (cd deps/blst
    git checkout bef14ca512ea575aff6f661fdad794263938795d
     ./build.sh -march=native)
fi

$CC -c sha/sha_ext_mbx2.S -o obj/sha_ext_mbx2.o

# Generate .h files for the Poseidon constants
xxd -i poseidon/constants/constants_2  > obj/constants_2.h
xxd -i poseidon/constants/constants_4  > obj/constants_4.h
xxd -i poseidon/constants/constants_8  > obj/constants_8.h
xxd -i poseidon/constants/constants_11 > obj/constants_11.h
xxd -i poseidon/constants/constants_16 > obj/constants_16.h
xxd -i poseidon/constants/constants_24 > obj/constants_24.h
xxd -i poseidon/constants/constants_36 > obj/constants_36.h

# PC1
$CXX $CXXFLAGS -Ideps/sppark/util -o obj/pc1.o -c pc1/pc1.cpp &

# PC2
$CXX $CXXFLAGS -o obj/streaming_node_reader_nvme.o -c nvme/streaming_node_reader_nvme.cpp &
$CXX $CXXFLAGS -o obj/ring_t.o -c nvme/ring_t.cpp &
$NVCC $CFLAGS $CUDA_ARCH -std=c++17 -DNO_SPDK -Xcompiler -march=native \
      -Xcompiler -Wall,-Wextra,-Wno-subobject-linkage,-Wno-unused-parameter \
      -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src -c pc2/cuda/pc2.cu -o obj/pc2.o &
# File-reader variant of pc2 for tree_r_file
$NVCC $CFLAGS $CUDA_ARCH -std=c++17 -DNO_SPDK -DSTREAMING_NODE_READER_FILES -DRENAME_PC2_HASH_FILES -Xcompiler -march=native \
      -Xcompiler -Wall,-Wextra,-Wno-subobject-linkage,-Wno-unused-parameter \
      -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src -c pc2/cuda/pc2.cu -o obj/pc2_files.o &

$CXX $CXXFLAGS $INCLUDE -Iposeidon -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src \
    -c sealing/supra_seal.cpp -o obj/supra_seal.o -Wno-subobject-linkage &

$CXX $CXXFLAGS $INCLUDE -DSTREAMING_NODE_READER_FILES -Iposeidon -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src \
    -c sealing/supra_tree_r_file.cpp -o obj/supra_tree_r_file.o -Wno-subobject-linkage &

wait

# Sppark object dedupe
nm obj/pc2.o | grep -E 'select_gpu|all_gpus|cuda_available|gpu_props|ngpus|drop_gpu_ptr_t|clone_gpu_ptr_t' | awk '{print $3 " supra_" $3}' > symbol_rename.txt
nm obj/pc2_files.o | grep -E 'select_gpu|all_gpus|cuda_available|gpu_props|ngpus|drop_gpu_ptr_t|clone_gpu_ptr_t' | awk '{print $3 " supra_" $3}' >> symbol_rename.txt
# Deduplicate symbol rename entries
sort -u -o symbol_rename.txt symbol_rename.txt

for obj in obj/pc1.o obj/pc2.o obj/pc2_files.o obj/ring_t.o obj/streaming_node_reader_nvme.o obj/supra_seal.o obj/supra_tree_r_file.o obj/sha_ext_mbx2.o; do
  objcopy --redefine-syms=symbol_rename.txt $obj
done

# Weaken duplicate symbols between pc2.o and pc2_files.o to avoid multiple-definition at link time
nm -g --defined-only obj/pc2.o | awk '{print $3}' | sort -u > obj/syms_pc2.txt
nm -g --defined-only obj/pc2_files.o | awk '{print $3}' | sort -u > obj/syms_pc2_files.txt
comm -12 obj/syms_pc2.txt obj/syms_pc2_files.txt | grep -v '^pc2_hash_files' > obj/syms_dups.txt
if [ -s obj/syms_dups.txt ]; then
  while read -r sym; do
    objcopy --weaken-symbol="$sym" obj/pc2_files.o
  done < obj/syms_dups.txt
fi

rm symbol_rename.txt

ar rvs obj/libsupraseal.a \
   obj/pc1.o \
   obj/pc2.o \
   obj/pc2_files.o \
   obj/ring_t.o \
   obj/streaming_node_reader_nvme.o \
   obj/supra_seal.o \
   obj/supra_tree_r_file.o \
   obj/sha_ext_mbx2.o

$CXX $CXXFLAGS -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src \
    -o bin/seal demos/main.cpp \
    -Lobj -lsupraseal \
    $LDFLAGS -Ldeps/blst -lblst -L$CUDA/lib64 -lcudart_static -lgmp -lconfig++ &

# tree-r CPU only
$CXX $SECTOR_SIZE $CXXSTD -pthread -g -O3 -march=native \
    -Wall -Wextra -Werror -Wno-subobject-linkage \
    tools/tree_r.cpp poseidon/poseidon.cpp \
    -o bin/tree_r_cpu -Iposeidon -Ideps/sppark -Ideps/blst/src -L deps/blst -lblst &

# tree-r CPU + GPU
$NVCC $SECTOR_SIZE -DNO_SPDK -DSTREAMING_NODE_READER_FILES \
     $CUDA_ARCH -std=c++17 -g -O3 -Xcompiler -march=native \
     -Xcompiler -Wall,-Wextra,-Werror \
     -Xcompiler -Wno-subobject-linkage,-Wno-unused-parameter \
     -x cu tools/tree_r.cpp -o bin/tree_r \
     -Iposeidon -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src -L deps/blst -lblst -lconfig++ &

# tree-d CPU only
$CXX -DRUNTIME_SECTOR_SIZE $CXXSTD -g -O3 -march=native \
    -Wall -Wextra -Werror -Wno-subobject-linkage \
    tools/tree_d.cpp \
    -o bin/tree_d_cpu -Ipc1 -L deps/blst -lblst &

# Standalone GPU pc2
$NVCC $SECTOR_SIZE -DNO_SPDK -DSTREAMING_NODE_READER_FILES \
     $CUDA_ARCH -std=c++17 -g -O3 -Xcompiler -march=native \
     -Xcompiler -Wall,-Wextra,-Werror \
     -Xcompiler -Wno-subobject-linkage,-Wno-unused-parameter \
     -x cu tools/tree_r.cpp -o bin/tree_r \
     -Iposeidon -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src -L deps/blst -lblst -lconfig++ &

# Standalone GPU pc2
$NVCC $SECTOR_SIZE -DNO_SPDK -DSTREAMING_NODE_READER_FILES \
     $CUDA_ARCH -std=c++17 -g -O3 -Xcompiler -march=native \
     -Xcompiler -Wall,-Wextra,-Werror \
     -Xcompiler -Wno-subobject-linkage,-Wno-unused-parameter \
     -x cu tools/pc2.cu -o bin/pc2 \
     -Iposeidon -Ideps/sppark -Ideps/sppark/util -Ideps/blst/src -L deps/blst -lblst -lconfig++ &

wait
