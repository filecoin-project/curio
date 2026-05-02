//! Emit GLSL field params + compile compute shaders to SPIR-V via `naga` (no system shaderc).

use std::collections::hash_map::DefaultHasher;
use std::env;
use std::fs;
use std::hash::Hasher;
use std::path::{Path, PathBuf};
use std::process::Command;

use blstrs::{Fp, Scalar};
use blst::blst_fr;
use ec_gpu::GpuName;
use ff::{Field, PrimeField};
use naga::back::spv;
use naga::front::glsl::{Frontend, Options};
use naga::valid::{Capabilities, ValidationFlags, Validator};
use naga::ShaderStage;

/// Fingerprint concatenated GLSL so `cargo:rustc-env` changes when sources change, forcing
/// `include_bytes!(…/g1_jacobian_add108.spv)` to re-embed (some incremental Cargo paths miss OUT_DIR).
fn glsl_embed_stamp(data: &str) -> u64 {
    let mut h = DefaultHasher::new();
    h.write(data.as_bytes());
    h.finish()
}

fn compile_glsl_compute(src: &str, dst: &Path) -> Result<(), Box<dyn std::error::Error>> {
    let mut frontend = Frontend::default();
    let options = Options::from(ShaderStage::Compute);
    let module = frontend
        .parse(&options, src)
        .map_err(|e| format!("naga glsl parse: {e:?}"))?;

    let mut validator = Validator::new(ValidationFlags::all(), Capabilities::all());
    let info = validator.validate(&module)?;

    let opts = spv::Options::default();
    let pipeline = spv::PipelineOptions {
        shader_stage: ShaderStage::Compute,
        entry_point: "main".into(),
    };
    let words = spv::write_vec(&module, &info, &opts, Some(&pipeline))?;
    let mut bytes = Vec::with_capacity(words.len() * 4);
    for w in words {
        bytes.extend_from_slice(&w.to_le_bytes());
    }
    fs::write(dst, bytes)?;
    Ok(())
}

/// Montgomery `u32[8]` limbs for `Scalar` (matches `cuzk_vk::scalar_limbs` / `blst_fr::l`).
fn mont_u32_limbs_build(s: &Scalar) -> [u32; 8] {
    let fr: blst_fr = (*s).into();
    let mut out = [0u32; 8];
    for i in 0..4 {
        out[2 * i] = fr.l[i] as u32;
        out[2 * i + 1] = (fr.l[i] >> 32) as u32;
    }
    out
}

/// `const uint NTT8_W*` twiddles for `n = 8` forward NTT (`fr_omega(8)` butterflies).
fn fr_ntt8_twiddle_const_glsl() -> String {
    let n = 8u64;
    let k = n.trailing_zeros();
    let omega = Scalar::ROOT_OF_UNITY.pow_vartime([1u64 << (Scalar::S - k)]);
    let w4 = omega.pow_vartime([2]);
    let w8 = omega.pow_vartime([1]);
    let w82 = omega.pow_vartime([2]);
    let w83 = omega.pow_vartime([3]);
    let line = |name: &str, s: &Scalar| {
        let limbs = mont_u32_limbs_build(s);
        let body = limbs
            .iter()
            .map(|u| format!("{}u", u))
            .collect::<Vec<_>>()
            .join(", ");
        format!("const uint {}[8] = uint[]({});\n", name, body)
    };
    let mut out = String::new();
    out.push_str(&line("NTT8_W4", &w4));
    out.push_str(&line("NTT8_W8", &w8));
    out.push_str(&line("NTT8_W82", &w82));
    out.push_str(&line("NTT8_W83", &w83));
    out
}

/// Inverse twiddles + `n^{-1}` (Montgomery) for `n = 8` inverse NTT (`fr_omega(8)^{-1}` butterflies).
fn fr_ntt8_twiddle_inv_const_glsl() -> String {
    let n = 8u64;
    let k = n.trailing_zeros();
    let omega = Scalar::ROOT_OF_UNITY.pow_vartime([1u64 << (Scalar::S - k)]);
    let omega_inv = omega.invert().unwrap();
    let w4 = omega_inv.pow_vartime([2]);
    let w8 = omega_inv.pow_vartime([1]);
    let w82 = omega_inv.pow_vartime([2]);
    let w83 = omega_inv.pow_vartime([3]);
    let n_inv = Scalar::from(n).invert().unwrap();
    let line = |name: &str, s: &Scalar| {
        let limbs = mont_u32_limbs_build(s);
        let body = limbs
            .iter()
            .map(|u| format!("{}u", u))
            .collect::<Vec<_>>()
            .join(", ");
        format!("const uint {}[8] = uint[]({});\n", name, body)
    };
    let mut out = String::new();
    out.push_str(&line("NTT8_INV_W4", &w4));
    out.push_str(&line("NTT8_INV_W8", &w8));
    out.push_str(&line("NTT8_INV_W82", &w82));
    out.push_str(&line("NTT8_INV_W83", &w83));
    out.push_str(&line("NTT8_NINV", &n_inv));
    out
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let out_dir = env::var("OUT_DIR")?;
    let out = Path::new(&out_dir);
    let fp = ec_gpu_gen::glsl_32_bit_field_params::<blstrs::Fp>();
    let fr = ec_gpu_gen::glsl_32_bit_field_params::<blstrs::Scalar>();
    fs::write(out.join("bls12_381_fp_params.glsl"), &fp)?;
    fs::write(out.join("bls12_381_fr_params.glsl"), &fr)?;

    let manifest = PathBuf::from(env::var("CARGO_MANIFEST_DIR")?);
    for (rel, spv_name) in [
        ("shaders/toy_ntt8.comp", "toy_ntt8.spv"),
        ("shaders/g1_reverse24.comp", "g1_reverse24.spv"),
        ("shaders/g2_reverse48.comp", "g2_reverse48.spv"),
    ] {
        let src_path = manifest.join(rel);
        let glsl = fs::read_to_string(&src_path)?;
        compile_glsl_compute(&glsl, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", src_path.display());
    }

    let spec_prebuilt = manifest.join("shaders/spec_constant_smoke_prebuilt.spv");
    fs::copy(&spec_prebuilt, &out.join("spec_constant_smoke.spv"))?;
    println!("cargo:rerun-if-changed={}", spec_prebuilt.display());
    let spec_glsl = manifest.join("shaders/spec_constant_smoke.comp");
    println!("cargo:rerun-if-changed={}", spec_glsl.display());

    let msm_prebuilt = manifest.join("shaders/msm_dispatch_grid_smoke_prebuilt.spv");
    fs::copy(&msm_prebuilt, &out.join("msm_dispatch_grid_smoke.spv"))?;
    println!("cargo:rerun-if-changed={}", msm_prebuilt.display());
    let msm_glsl = manifest.join("shaders/msm_dispatch_grid_smoke.comp");
    println!("cargo:rerun-if-changed={}", msm_glsl.display());

    let fr_t = Scalar::name();
    let fr_p = format!("{}_P", fr_t);
    let fr_inv = format!("{}_INV", fr_t);
    for (tail_name, spv_name) in [
        ("fr_add8_tail.comp", "fr_add8.spv"),
        ("fr_mul8_tail.comp", "fr_mul8.spv"),
        ("fr_sub8_tail.comp", "fr_sub8.spv"),
    ] {
        let tail_path = manifest.join("shaders").join(tail_name);
        let tail = fs::read_to_string(&tail_path)?
            .replace("@@FR_T@@", &fr_t)
            .replace("@@FR_P@@", &fr_p)
            .replace("@@FR_INV@@", &fr_inv)
            .replace("@@FR_ONE@@", &format!("{}_ONE", fr_t));
        let full = format!("{}\n{}", &fr, tail);
        compile_glsl_compute(&full, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", tail_path.display());
    }

    let fp_t = Fp::name();
    let fp_p = format!("{}_P", fp_t);
    let fp_inv = format!("{}_INV", fp_t);
    for (tail_name, spv_name) in [
        ("fp_add12_tail.comp", "fp_add12.spv"),
        ("fp_sub12_tail.comp", "fp_sub12.spv"),
        ("fp_mul12_tail.comp", "fp_mul12.spv"),
    ] {
        let tail_path = manifest.join("shaders").join(tail_name);
        let tail = fs::read_to_string(&tail_path)?
            .replace("@@FP_T@@", &fp_t)
            .replace("@@FP_P@@", &fp_p)
            .replace("@@FP_INV@@", &fp_inv)
            .replace("@@FP_ONE@@", &format!("{}_ONE", fp_t));
        let full = format!("{}\n{}", &fp, tail);
        compile_glsl_compute(&full, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", tail_path.display());
    }

    let fp_helpers_path = manifest.join("shaders/fp_helpers.glsl");
    let fp_helpers = fs::read_to_string(&fp_helpers_path)?
        .replace("@@FP_T@@", &fp_t)
        .replace("@@FP_P@@", &fp_p)
        .replace("@@FP_INV@@", &fp_inv)
        .replace("@@FP_ONE@@", &format!("{}_ONE", fp_t));
    println!("cargo:rerun-if-changed={}", fp_helpers_path.display());

    for (tail_name, spv_name) in [
        ("fp2_add24_tail.comp", "fp2_add24.spv"),
        ("fp2_sub24_tail.comp", "fp2_sub24.spv"),
        ("fp2_mul24_tail.comp", "fp2_mul24.spv"),
        ("fp2_sqr24_tail.comp", "fp2_sqr24.spv"),
    ] {
        let tail_path = manifest.join("shaders").join(tail_name);
        let tail = fs::read_to_string(&tail_path)?
            .replace("@@FP_T@@", &fp_t)
            .replace("@@FP_P@@", &fp_p)
            .replace("@@FP_INV@@", &fp_inv)
            .replace("@@FP_ONE@@", &format!("{}_ONE", fp_t));
        let full = format!("{}\n{}\n{}", &fp, fp_helpers, tail);
        compile_glsl_compute(&full, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", tail_path.display());
    }

    for (tail_name, spv_name) in [
        ("g1_jacobian_add108_tail.comp", "g1_jacobian_add108.spv"),
        ("g1_jacobian_dbl108_tail.comp", "g1_jacobian_dbl108.spv"),
        ("g1_xyzz_add_mixed72_tail.comp", "g1_xyzz_add_mixed72.spv"),
        ("g1_batch_accum_bitmap1636_tail.comp", "g1_batch_accum_bitmap1636.spv"),
        ("g1_bucket_sum_window4_n32_tail.comp", "g1_bucket_sum_window4_n32.spv"),
        ("g1_pippenger_bucket_acc_tail.comp", "g1_pippenger_bucket_acc.spv"),
        ("g1_mega_strip_circuit_hit_tail.comp", "g1_mega_strip_circuit_hit.spv"),
        ("gpu_echo_u32_tail.comp", "gpu_echo_u32.spv"),
    ] {
        let tail_path = manifest.join("shaders").join(tail_name);
        let tail = fs::read_to_string(&tail_path)?
            .replace("@@FP_T@@", &fp_t)
            .replace("@@FP_P@@", &fp_p)
            .replace("@@FP_INV@@", &fp_inv)
            .replace("@@FP_ONE@@", &format!("{}_ONE", fp_t));
        let full = format!("{}\n{}\n{}", &fp, fp_helpers, tail);
        compile_glsl_compute(&full, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", tail_path.display());
        if spv_name == "g1_jacobian_add108.spv" {
            println!(
                "cargo:rustc-env=CUZK_VK_G1_JAC_ADD108_STAMP={:016x}",
                glsl_embed_stamp(&full)
            );
        }
    }

    let fp2_ops_path = manifest.join("shaders/fp2_mont_ops.glsl");
    let fp2_ops = fs::read_to_string(&fp2_ops_path)?.replace("@@FP_T@@", &fp_t);
    println!("cargo:rerun-if-changed={}", fp2_ops_path.display());

    for (tail_name, spv_name) in [
        ("g2_jacobian_add216_tail.comp", "g2_jacobian_add216.spv"),
        ("g2_xyzz_add_mixed144_tail.comp", "g2_xyzz_add_mixed144.spv"),
        ("g2_batch_accum_aff16_904_tail.comp", "g2_batch_accum_aff16_904.spv"),
    ] {
        let tail_path = manifest.join("shaders").join(tail_name);
        let tail = fs::read_to_string(&tail_path)?
            .replace("@@FP_T@@", &fp_t)
            .replace("@@FP_P@@", &fp_p)
            .replace("@@FP_INV@@", &fp_inv)
            .replace("@@FP_ONE@@", &format!("{}_ONE", fp_t));
        let full = format!("{}\n{}\n{}\n{}", &fp, fp_helpers, fp2_ops, tail);
        compile_glsl_compute(&full, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", tail_path.display());
        if spv_name == "g2_jacobian_add216.spv" {
            println!(
                "cargo:rustc-env=CUZK_VK_G2_JAC_ADD216_STAMP={:016x}",
                glsl_embed_stamp(&full)
            );
        }
    }

    let ntt8_path = manifest.join("shaders/fr_ntt8_forward_tail.comp");
    let ntt8_tail = fs::read_to_string(&ntt8_path)?
        .replace("@@FR_T@@", &fr_t)
        .replace("@@FR_P@@", &fr_p)
        .replace("@@FR_INV@@", &fr_inv)
        .replace("@@FR_ONE@@", &format!("{}_ONE", fr_t));
    let ntt8_glsl = format!("{}\n{}\n{}", &fr, fr_ntt8_twiddle_const_glsl(), ntt8_tail);
    compile_glsl_compute(&ntt8_glsl, &out.join("fr_ntt8_forward.spv"))?;
    println!("cargo:rerun-if-changed={}", ntt8_path.display());

    let ntt8_inv_path = manifest.join("shaders/fr_ntt8_inverse_tail.comp");
    let ntt8_inv_tail = fs::read_to_string(&ntt8_inv_path)?
        .replace("@@FR_T@@", &fr_t)
        .replace("@@FR_P@@", &fr_p)
        .replace("@@FR_INV@@", &fr_inv)
        .replace("@@FR_ONE@@", &format!("{}_ONE", fr_t));
    let ntt8_inv_glsl = format!(
        "{}\n{}\n{}",
        &fr,
        fr_ntt8_twiddle_inv_const_glsl(),
        ntt8_inv_tail
    );
    compile_glsl_compute(&ntt8_inv_glsl, &out.join("fr_ntt8_inverse.spv"))?;
    println!("cargo:rerun-if-changed={}", ntt8_inv_path.display());

    let fr_helpers_path = manifest.join("shaders/fr_helpers.glsl");
    let fr_helpers = fs::read_to_string(&fr_helpers_path)?
        .replace("@@FR_T@@", &fr_t)
        .replace("@@FR_P@@", &fr_p)
        .replace("@@FR_INV@@", &fr_inv)
        .replace("@@FR_ONE@@", &format!("{}_ONE", fr_t));
    println!("cargo:rerun-if-changed={}", fr_helpers_path.display());

    for (tail_name, spv_name) in [
        ("fr_coeff_wise_mult_tail.comp", "fr_coeff_wise_mult.spv"),
        ("fr_sub_mult_constant_tail.comp", "fr_sub_mult_constant.spv"),
    ] {
        let tail_path = manifest.join("shaders").join(tail_name);
        let tail = fs::read_to_string(&tail_path)?
            .replace("@@FR_T@@", &fr_t)
            .replace("@@FR_P@@", &fr_p)
            .replace("@@FR_INV@@", &fr_inv)
            .replace("@@FR_ONE@@", &format!("{}_ONE", fr_t));
        let full = format!("{}\n{}\n{}", &fr, fr_helpers, tail);
        compile_glsl_compute(&full, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", tail_path.display());
    }

    for (tail_name, spv_name) in [
        ("fr_ntt_general_bitrev_scatter_tail.comp", "fr_ntt_general_bitrev_scatter.spv"),
        (
            "fr_ntt_general_copy_scratch_to_data_tail.comp",
            "fr_ntt_general_copy_scratch_to_data.spv",
        ),
        ("fr_ntt_general_radix2_stage_tail.comp", "fr_ntt_general_radix2_stage.spv"),
        ("fr_ntt_general_scale_ninv_tail.comp", "fr_ntt_general_scale_ninv.spv"),
    ] {
        let tail_path = manifest.join("shaders").join(tail_name);
        let tail = fs::read_to_string(&tail_path)?
            .replace("@@FR_T@@", &fr_t)
            .replace("@@FR_P@@", &fr_p)
            .replace("@@FR_INV@@", &fr_inv)
            .replace("@@FR_ONE@@", &format!("{}_ONE", fr_t));
        let full = format!("{}\n{}\n{}", &fr, fr_helpers, tail);
        compile_glsl_compute(&full, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", tail_path.display());
    }

    // Optional glslang path: radix-2 stage with `local_size_x_id = 0` (host passes WG=256 as spec).
    let radix2_spec_tail_path = manifest.join("shaders/fr_ntt_general_radix2_stage_spec_tail.comp");
    let radix2_spec_tail = fs::read_to_string(&radix2_spec_tail_path)?
        .replace("@@FR_T@@", &fr_t)
        .replace("@@FR_P@@", &fr_p)
        .replace("@@FR_INV@@", &fr_inv)
        .replace("@@FR_ONE@@", &format!("{}_ONE", fr_t));
    let radix2_spec_glsl = out.join("fr_ntt_radix2_stage_spec_full.glsl");
    fs::write(
        &radix2_spec_glsl,
        format!("#version 450\n{}\n{}\n{}", &fr, fr_helpers, radix2_spec_tail),
    )?;
    let radix2_spec_spv = out.join("fr_ntt_general_radix2_stage_spec.spv");
    let glslang_ok = Command::new("glslangValidator")
        .args(["-V", "-S", "comp", "-o"])
        .arg(&radix2_spec_spv)
        .arg(&radix2_spec_glsl)
        .status()
        .map(|s| s.success())
        .unwrap_or(false);
    if glslang_ok {
        println!("cargo:rustc-cfg=fr_ntt_radix2_spec");
    } else {
        fs::copy(out.join("fr_ntt_general_radix2_stage.spv"), &radix2_spec_spv)?;
    }
    println!("cargo:rerun-if-changed={}", radix2_spec_tail_path.display());

    println!("cargo:rerun-if-changed=build.rs");
    Ok(())
}
