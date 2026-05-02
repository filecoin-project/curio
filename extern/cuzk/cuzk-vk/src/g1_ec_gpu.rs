//! G1 Jacobian / XYZZ point ops on the GPU (Montgomery `Fp` limbs).

use std::io::Cursor;
use std::sync::OnceLock;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::G1Affine;

use crate::device::VulkanDevice;
use crate::ec::{
    g1_affine_montgomery_limbs, G1JacobianLimbs, G1XyzzLimbs, G1_JACOBIAN_ADD_SSBO_BYTES,
    G1_XYZZ_ADD_MIXED_SSBO_BYTES,
};
use crate::g1::BLS12_381_FP_U32_LIMBS;
use crate::vk_oneshot;

const JAC_BUF: u64 = 512;
const XYZZ_BUF: u64 = 512;

/// Read SPIR-V from `OUT_DIR` at runtime instead of `include_bytes!` so a stale crate `rmeta`
/// (incremental Cargo can keep the `.rlib` while `build.rs` rewrites the `.spv`) cannot pin an
/// out-of-date kernel. `OUT_DIR` itself is baked at compile time but is stable per package.
fn load_out_dir_spv(name: &'static str) -> &'static [u8] {
    static CACHE: OnceLock<std::sync::Mutex<std::collections::HashMap<&'static str, &'static [u8]>>> =
        OnceLock::new();
    let map = CACHE.get_or_init(Default::default);
    let mut guard = map.lock().expect("spv cache");
    if let Some(slice) = guard.get(name) {
        return slice;
    }
    let path = format!("{}/{}", env!("OUT_DIR"), name);
    let bytes = std::fs::read(&path)
        .unwrap_or_else(|e| panic!("read SPIR-V {}: {}", path, e));
    let mut h: u64 = 0xcbf29ce484222325;
    for &b in bytes.iter() {
        h ^= b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    eprintln!(
        "[cuzk-vk] loaded SPIR-V {} ({} bytes, fnv1a64=0x{:016x}) from {}",
        name, bytes.len(), h, path
    );
    let leaked: &'static [u8] = Box::leak(bytes.into_boxed_slice());
    guard.insert(name, leaked);
    leaked
}

fn put_fp12(buf: &mut [u8], off_u32: usize, limbs: &[u32; BLS12_381_FP_U32_LIMBS]) {
    for i in 0..BLS12_381_FP_U32_LIMBS {
        let o = (off_u32 + i) * 4;
        buf[o..o + 4].copy_from_slice(&limbs[i].to_le_bytes());
    }
}

fn get_fp12(buf: &[u8], off_u32: usize) -> [u32; BLS12_381_FP_U32_LIMBS] {
    let mut out = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        let o = (off_u32 + i) * 4;
        out[i] = u32::from_le_bytes(buf[o..o + 4].try_into().unwrap());
    }
    out
}

/// `Z == 0` (point at infinity) check on Jacobian limbs (Mont form: zero is zero).
fn is_jac_identity(p: &G1JacobianLimbs) -> bool {
    p.z.iter().all(|&w| w == 0)
}

/// True iff `a == b` as Jacobian limbs (so doubling rather than addition is the
/// correct law to dispatch). This is a stricter condition than "same affine
/// point": callers that want strict point-equality detection (e.g. via
/// projective comparison) must canonicalise first. For the prover usage we
/// accumulate distinct random scalars, so identical limb tuples reliably
/// indicate "this is the doubling case" (`a + a` test, etc.).
fn jac_eq(a: &G1JacobianLimbs, b: &G1JacobianLimbs) -> bool {
    a.x == b.x && a.y == b.y && a.z == b.z
}

/// Dispatch the dedicated doubling shader on `a` (input `b` is ignored).
fn run_g1_jacobian_dbl_gpu(
    dev: &VulkanDevice,
    a: &G1JacobianLimbs,
) -> Result<G1JacobianLimbs> {
    let spirv = load_out_dir_spv("g1_jacobian_dbl108.spv");
    let spirv_words =
        read_spv(&mut Cursor::new(spirv)).context("read_spv g1_jacobian_dbl108")?;
    let mut wbytes = [0u8; G1_JACOBIAN_ADD_SSBO_BYTES];
    put_fp12(&mut wbytes, 36, &a.x);
    put_fp12(&mut wbytes, 48, &a.y);
    put_fp12(&mut wbytes, 60, &a.z);
    let mut out = [0u8; (BLS12_381_FP_U32_LIMBS * 3) * 4];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            JAC_BUF,
            JAC_BUF,
            (1, 1, 1),
            &wbytes,
            out.len(),
            &mut out,
            None,
        )?;
    }
    Ok(G1JacobianLimbs {
        x: get_fp12(&out, 0),
        y: get_fp12(&out, 12),
        z: get_fp12(&out, 24),
    })
}

/// `a + b` on Jacobian points (Montgomery I/O).
///
/// The SPIR-V kernels are kept straight-line because Naga + MoltenVK on Apple
/// M2 silently corrupts large `@@FP_T@@`-typed locals across `if` bodies (see
/// the shader header). Routing of identity / doubling cases is therefore done
/// here on the host, dispatching the addition kernel for the common case and
/// the dedicated doubling kernel only when `a == b`.
pub fn run_g1_jacobian_add_gpu(
    dev: &VulkanDevice,
    a: &G1JacobianLimbs,
    b: &G1JacobianLimbs,
) -> Result<G1JacobianLimbs> {
    if is_jac_identity(a) {
        return Ok(*b);
    }
    if is_jac_identity(b) {
        return Ok(*a);
    }
    if jac_eq(a, b) {
        return run_g1_jacobian_dbl_gpu(dev, a);
    }
    let spirv = load_out_dir_spv("g1_jacobian_add108.spv");
    let spirv_words =
        read_spv(&mut Cursor::new(spirv)).context("read_spv g1_jacobian_add108")?;
    let mut wbytes = [0u8; G1_JACOBIAN_ADD_SSBO_BYTES];
    put_fp12(&mut wbytes, 36, &a.x);
    put_fp12(&mut wbytes, 48, &a.y);
    put_fp12(&mut wbytes, 60, &a.z);
    put_fp12(&mut wbytes, 72, &b.x);
    put_fp12(&mut wbytes, 84, &b.y);
    put_fp12(&mut wbytes, 96, &b.z);
    let mut out = [0u8; (BLS12_381_FP_U32_LIMBS * 3) * 4];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            JAC_BUF,
            JAC_BUF,
            (1, 1, 1),
            &wbytes,
            out.len(),
            &mut out,
            None,
        )?;
    }
    Ok(G1JacobianLimbs {
        x: get_fp12(&out, 0),
        y: get_fp12(&out, 12),
        z: get_fp12(&out, 24),
    })
}

/// XYZZ += affine `p2` (Montgomery I/O). `p2` must not be the point at infinity.
pub fn run_g1_xyzz_add_mixed_gpu(dev: &VulkanDevice, xyzz: &mut G1XyzzLimbs, p2: &G1Affine) -> Result<()> {
    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_xyzz_add_mixed72.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g1_xyzz_add_mixed72")?;
    let (p2x, p2y) = g1_affine_montgomery_limbs(p2);
    let mut wbytes = [0u8; G1_XYZZ_ADD_MIXED_SSBO_BYTES];
    put_fp12(&mut wbytes, 0, &xyzz.x);
    put_fp12(&mut wbytes, 12, &xyzz.y);
    put_fp12(&mut wbytes, 24, &xyzz.zzz);
    put_fp12(&mut wbytes, 36, &xyzz.zz);
    put_fp12(&mut wbytes, 48, &p2x);
    put_fp12(&mut wbytes, 60, &p2y);
    let read_len = 48 * 4;
    let mut out = [0u8; 48 * 4];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            XYZZ_BUF,
            XYZZ_BUF,
            (1, 1, 1),
            &wbytes,
            read_len,
            &mut out,
            None,
        )?;
    }
    xyzz.x = get_fp12(&out, 0);
    xyzz.y = get_fp12(&out, 12);
    xyzz.zzz = get_fp12(&out, 24);
    xyzz.zz = get_fp12(&out, 36);
    Ok(())
}
