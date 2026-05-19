//! Step 1.2: field parameter emission + CPU oracle (GPU shader dispatch comes later).

use blstrs::{Fp, Scalar};
use ec_gpu::{GpuField, GpuName};
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn glsl_fp() -> &'static str {
    include_str!(concat!(env!("OUT_DIR"), "/bls12_381_fp_params.glsl"))
}

fn glsl_fr() -> &'static str {
    include_str!(concat!(env!("OUT_DIR"), "/bls12_381_fr_params.glsl"))
}

fn parse_const_u32_array(glsl: &str, array_name: &str) -> Option<Vec<u32>> {
    let prefix = format!("const uint {array_name}[");
    let i = glsl.find(&prefix)?;
    let mut idx = i + prefix.len();
    let size_str: String = glsl[idx..]
        .chars()
        .take_while(|c| c.is_ascii_digit())
        .collect();
    let n: usize = size_str.parse().ok()?;
    idx += size_str.len();
    let tail = glsl.get(idx..)?;
    let open = "] = uint[](";
    let j = tail.find(open)?;
    idx += j + open.len();
    let end_paren = glsl[idx..].find(')')? + idx;
    let inner = glsl.get(idx..end_paren)?;
    let mut out = Vec::with_capacity(n);
    for tok in inner.split(',') {
        let t = tok.trim();
        let t = t.strip_suffix('u').unwrap_or(t).trim();
        out.push(t.parse().ok()?);
    }
    (out.len() == n).then_some(out)
}

#[test]
fn generated_glsl_matches_ec_gpu_gen() {
    let fp_disk = glsl_fp();
    let fp_live = ec_gpu_gen::glsl_32_bit_field_params::<Fp>();
    assert_eq!(fp_disk, fp_live);

    let fr_disk = glsl_fr();
    let fr_live = ec_gpu_gen::glsl_32_bit_field_params::<Scalar>();
    assert_eq!(fr_disk, fr_live);
}

#[test]
fn glsl_modulus_arrays_match_gpu_field_trait() {
    let fp_glsl = glsl_fp();
    let fp_name = Fp::name();
    let p_glsl = parse_const_u32_array(fp_glsl, &format!("{fp_name}_P")).expect("parse Fp P");
    assert_eq!(p_glsl, Fp::modulus());

    let fr_glsl = glsl_fr();
    let fr_name = Scalar::name();
    let r_glsl = parse_const_u32_array(fr_glsl, &format!("{fr_name}_P")).expect("parse Fr P");
    assert_eq!(r_glsl, Scalar::modulus());
}

#[test]
fn fp_cpu_random_ops_1024() {
    let mut rng = ChaCha8Rng::seed_from_u64(0xC0FFEE);
    for _ in 0..1024 {
        let a = Fp::random(&mut rng);
        let b = Fp::random(&mut rng);
        let sum = a + b;
        let prod = a * b;
        let sq = a.square();
        assert_eq!(sum - b, a);
        assert_eq!(prod * b.invert().unwrap(), a);
        assert_eq!(sq, a * a);
    }
}

#[test]
fn fr_cpu_random_ops_1024() {
    let mut rng = ChaCha8Rng::seed_from_u64(0x51AD);
    for _ in 0..1024 {
        let a = Scalar::random(&mut rng);
        let b = Scalar::random(&mut rng);
        let sum = a + b;
        let prod = a * b;
        let sq = a.square();
        assert_eq!(sum - b, a);
        assert_eq!(prod * b.invert().unwrap(), a);
        assert_eq!(sq, a * a);
    }
}
