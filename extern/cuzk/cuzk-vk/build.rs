//! Emit GLSL field params + compile compute shaders to SPIR-V via `naga` (no system shaderc).

use std::env;
use std::fs;
use std::path::{Path, PathBuf};

use blstrs::Scalar;
use blst::blst_fr;
use ec_gpu::GpuName;
use ff::{Field, PrimeField};
use naga::back::spv;
use naga::front::glsl::{Frontend, Options};
use naga::valid::{Capabilities, ValidationFlags, Validator};
use naga::ShaderStage;

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
    fs::write(out.join("bls12_381_fp_params.glsl"), fp)?;
    fs::write(out.join("bls12_381_fr_params.glsl"), &fr)?;

    let manifest = PathBuf::from(env::var("CARGO_MANIFEST_DIR")?);
    for (rel, spv_name) in [
        ("shaders/toy_ntt8.comp", "toy_ntt8.spv"),
        ("shaders/g1_reverse24.comp", "g1_reverse24.spv"),
        ("shaders/g2_reverse48.comp", "g2_reverse48.spv"),
        ("shaders/msm_dispatch_grid_smoke.comp", "msm_dispatch_grid_smoke.spv"),
    ] {
        let src_path = manifest.join(rel);
        let glsl = fs::read_to_string(&src_path)?;
        compile_glsl_compute(&glsl, &out.join(spv_name))?;
        println!("cargo:rerun-if-changed={}", src_path.display());
    }

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

    println!("cargo:rerun-if-changed=build.rs");
    Ok(())
}
