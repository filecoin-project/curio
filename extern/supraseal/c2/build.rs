use std::env;

fn main() {
    groth16_cuda();
}

fn groth16_cuda() {
    let mut nvcc = cc::Build::new();
    nvcc.cuda(true);
    nvcc.flag("-arch=sm_80");
    nvcc.flag("-gencode").flag("arch=compute_70,code=sm_70");
    nvcc.flag("-t0");
    nvcc.define("TAKE_RESPONSIBILITY_FOR_ERROR_MESSAGE", None);
    nvcc.define("FEATURE_BLS12_381", None);
    apply_blst_flags(&mut nvcc);
    if let Some(include) = env::var_os("DEP_BLST_C_SRC") {
        nvcc.include(&include);
    }
    if let Some(include) = env::var_os("DEP_SPPARK_ROOT") {
        nvcc.include(include);
    }
    nvcc.flag("-Xcompiler").flag("-Wno-subobject-linkage");
    nvcc.flag("-Xcompiler").flag("-Wno-unused-function");
    // Explicitly set C++17 standard for NVCC to handle __constant__ variables properly
    nvcc.flag("-std=c++17");

    nvcc.file("cuda/groth16_cuda.cu").compile("groth16_cuda");

    println!("cargo:rerun-if-changed=cuda");
    println!("cargo:rerun-if-env-changed=CXXFLAGS");
}

fn apply_blst_flags(nvcc: &mut cc::Build) {
    let target_arch = env::var("CARGO_CFG_TARGET_ARCH").unwrap();

    match (cfg!(feature = "portable"), cfg!(feature = "force-adx")) {
        (true, false) => {
            nvcc.define("__BLST_PORTABLE__", None);
        }
        (false, true) => {
            if target_arch.eq("x86_64") {
                nvcc.define("__ADX__", None);
            }
        }
        (false, false) =>
        {
            #[cfg(target_arch = "x86_64")]
            if target_arch.eq("x86_64") && std::is_x86_feature_detected!("adx") {
                nvcc.define("__ADX__", None);
            }
        }
        (true, true) => panic!("Cannot compile with both `portable` and `force-adx` features"),
    }
}
