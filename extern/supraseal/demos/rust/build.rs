// Copyright Supranational LLC

use std::path::PathBuf;

fn main() {
    let cpp_lib_dir = PathBuf::from("../../obj")
        .canonicalize()
        .expect("cannot canonicalize path");

    let spdk_lib_dir_buf = PathBuf::from("../../deps/spdk-v22.09/build/lib")
        .canonicalize()
        .expect("cannot canonicalize path");
    let spdk_lib_dir = spdk_lib_dir_buf.to_str().unwrap();

    let dpdk_lib_dir_buf = PathBuf::from("../../deps/spdk-v22.09/dpdk/build/lib")
        .canonicalize()
        .expect("cannot canonicalize path");
    let dpdk_lib_dir = dpdk_lib_dir_buf.to_str().unwrap();

    let dpdk_env_path = PathBuf::from("../../deps/spdk-v22.09/build/lib/libspdk_env_dpdk.a")
        .canonicalize()
        .expect("cannot canonicalize path");

    println!("cargo:rustc-link-search={}", cpp_lib_dir.to_str().unwrap());
    println!("cargo:rustc-link-search={}", spdk_lib_dir);

    println!("cargo:rustc-link-arg=-fno-omit-frame-pointer");
    println!("cargo:rustc-link-arg=-Wl,-z,relro,-z,now");
    println!("cargo:rustc-link-arg=-Wl,-z,noexecstack");
    println!("cargo:rustc-link-arg=-fuse-ld=bfd");
    println!("cargo:rustc-link-arg=-Wl,--whole-archive");
    println!("cargo:rustc-link-arg=-Wl,--no-as-needed");
    println!("cargo:rustc-link-arg=-lspdk_bdev_malloc");
    println!("cargo:rustc-link-arg=-lspdk_bdev_null");
    println!("cargo:rustc-link-arg=-lspdk_bdev_nvme");
    println!("cargo:rustc-link-arg=-lspdk_bdev_passthru");
    println!("cargo:rustc-link-arg=-lspdk_bdev_lvol");
    println!("cargo:rustc-link-arg=-lspdk_bdev_raid");
    println!("cargo:rustc-link-arg=-lspdk_bdev_error");
    println!("cargo:rustc-link-arg=-lspdk_bdev_gpt");
    println!("cargo:rustc-link-arg=-lspdk_bdev_split");
    println!("cargo:rustc-link-arg=-lspdk_bdev_delay");
    println!("cargo:rustc-link-arg=-lspdk_bdev_zone_block");
    println!("cargo:rustc-link-arg=-lspdk_blobfs_bdev");
    println!("cargo:rustc-link-arg=-lspdk_blobfs");
    println!("cargo:rustc-link-arg=-lspdk_blob_bdev");
    println!("cargo:rustc-link-arg=-lspdk_lvol");
    println!("cargo:rustc-link-arg=-lspdk_blob");
    println!("cargo:rustc-link-arg=-lspdk_nvme");
    println!("cargo:rustc-link-arg=-lspdk_bdev_ftl");
    println!("cargo:rustc-link-arg=-lspdk_ftl");
    println!("cargo:rustc-link-arg=-lspdk_bdev_aio");
    println!("cargo:rustc-link-arg=-lspdk_bdev_virtio");
    println!("cargo:rustc-link-arg=-lspdk_virtio");
    println!("cargo:rustc-link-arg=-lspdk_vfio_user");
    println!("cargo:rustc-link-arg=-lspdk_accel_ioat");
    println!("cargo:rustc-link-arg=-lspdk_ioat");
    println!("cargo:rustc-link-arg=-lspdk_scheduler_dynamic");
    println!("cargo:rustc-link-arg=-lspdk_env_dpdk");
    println!("cargo:rustc-link-arg=-lspdk_scheduler_dpdk_governor");
    println!("cargo:rustc-link-arg=-lspdk_scheduler_gscheduler");
    println!("cargo:rustc-link-arg=-lspdk_sock_posix");
    println!("cargo:rustc-link-arg=-lspdk_event");
    println!("cargo:rustc-link-arg=-lspdk_event_bdev");
    println!("cargo:rustc-link-arg=-lspdk_bdev");
    println!("cargo:rustc-link-arg=-lspdk_notify");
    println!("cargo:rustc-link-arg=-lspdk_dma");
    println!("cargo:rustc-link-arg=-lspdk_event_accel");
    println!("cargo:rustc-link-arg=-lspdk_accel");
    println!("cargo:rustc-link-arg=-lspdk_event_vmd");
    println!("cargo:rustc-link-arg=-lspdk_vmd");
    println!("cargo:rustc-link-arg=-lspdk_event_sock");
    println!("cargo:rustc-link-arg=-lspdk_init");
    println!("cargo:rustc-link-arg=-lspdk_thread");
    println!("cargo:rustc-link-arg=-lspdk_trace");
    println!("cargo:rustc-link-arg=-lspdk_sock");
    println!("cargo:rustc-link-arg=-lspdk_rpc");
    println!("cargo:rustc-link-arg=-lspdk_jsonrpc");
    println!("cargo:rustc-link-arg=-lspdk_json");
    println!("cargo:rustc-link-arg=-lspdk_util");
    println!("cargo:rustc-link-arg=-lspdk_log");
    println!("cargo:rustc-link-arg=-Wl,--no-whole-archive");
    println!("cargo:rustc-link-arg={}", dpdk_env_path.to_str().unwrap());
    println!("cargo:rustc-link-arg=-Wl,--whole-archive");
    println!("cargo:rustc-link-arg={}/librte_bus_pci.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_cryptodev.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_dmadev.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_eal.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_ethdev.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_hash.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_kvargs.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_mbuf.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_mempool.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_mempool_ring.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_net.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_pci.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_power.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_rcu.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_ring.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_telemetry.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg={}/librte_vhost.a", dpdk_lib_dir);
    println!("cargo:rustc-link-arg=-Wl,--no-whole-archive");
    println!("cargo:rustc-link-arg=-lnuma");
    println!("cargo:rustc-link-arg=-ldl");
    println!("cargo:rustc-link-arg=-L{}/../../isa-l/.libs", spdk_lib_dir);
    println!("cargo:rustc-link-arg=-lisal");
    println!("cargo:rustc-link-arg=-pthread");
    println!("cargo:rustc-link-arg=-lrt");
    println!("cargo:rustc-link-arg=-luuid");
    println!("cargo:rustc-link-arg=-lssl");
    println!("cargo:rustc-link-arg=-lcrypto");
    println!("cargo:rustc-link-arg=-lm");
    println!("cargo:rustc-link-arg=-laio");
    println!("cargo:rustc-link-arg=-lc");
    println!("cargo:rustc-link-arg=-lgcc");

    println!("cargo:rustc-link-lib=supraseal");
    println!("cargo:rustc-link-lib=gmp");
    println!("cargo:rustc-link-lib=config++");
    println!("cargo:rustc-link-lib=static:-bundle=stdc++");

    println!("cargo:rerun-if-changed={}", cpp_lib_dir.to_str().unwrap());

}
