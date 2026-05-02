//! Phase G: bitmap-selected sum of affine G2 points.
//!
//! The host iterates the selected affine points and dispatches the per-add G2 Jacobian
//! kernel ([`crate::g2_ec_gpu::run_g2_jacobian_add_gpu`]) for each one. An earlier
//! single-workgroup shared-memory tree-reduction shader hit unrecoverable Naga + MoltenVK
//! codegen bugs on Apple M2 (large `@@FP_T@@`-typed locals corrupted across `if` bodies
//! when the loop body contained both an identity short-circuit and the EFD addition).
//! With `n ≤ 16` the per-add dispatch overhead is negligible.

use anyhow::Result;
use blstrs::{G2Affine, G2Projective};
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::{
    g2_affine_montgomery_limbs, g2_jacobian_limbs_from_projective, G2JacobianLimbs,
    G2_BATCH_ACC_MAX_POINTS,
};
use crate::fp2::BLS12_381_FP2_U32_LIMBS;
use crate::g2_ec_gpu::run_g2_jacobian_add_gpu;

fn jac_identity() -> G2JacobianLimbs {
    G2JacobianLimbs {
        x: [0u32; BLS12_381_FP2_U32_LIMBS],
        y: [0u32; BLS12_381_FP2_U32_LIMBS],
        z: [0u32; BLS12_381_FP2_U32_LIMBS],
    }
}

fn affine_to_jac(p: &G2Affine) -> G2JacobianLimbs {
    let (x, y) = g2_affine_montgomery_limbs(p);
    // Z = Mont(1) for the Y, Z layout used by the Jacobian add shader. Translate via
    // `g2_jacobian_limbs_from_projective(G2Projective::from(p))` to share the canonical
    // Mont-1 encoding with [`g2_jacobian_limbs_from_projective`].
    let z = {
        let proj = G2Projective::from(*p);
        g2_jacobian_limbs_from_projective(&proj).z
    };
    G2JacobianLimbs { x, y, z }
}

/// Sum affine `points[0..n)` where bit *i* of `bitmap` is set; `n <= 16` (low bits only).
pub fn run_g2_batch_jacobian_accum_bitmap_gpu(
    dev: &VulkanDevice,
    n: usize,
    bitmap: u32,
    points: &[G2Affine],
) -> Result<G2JacobianLimbs> {
    anyhow::ensure!(n <= G2_BATCH_ACC_MAX_POINTS);
    anyhow::ensure!(points.len() >= n);
    let mut acc = jac_identity();
    for i in 0..n {
        if ((bitmap >> i) & 1) == 0 {
            continue;
        }
        let p = affine_to_jac(&points[i]);
        acc = run_g2_jacobian_add_gpu(dev, &acc, &p)?;
    }
    Ok(acc)
}

/// CPU oracle for [`run_g2_batch_jacobian_accum_bitmap_gpu`].
pub fn g2_batch_jacobian_accum_bitmap_cpu(n: usize, bitmap: u32, points: &[G2Affine]) -> G2JacobianLimbs {
    let mut acc = G2Projective::identity();
    for i in 0..n {
        if ((bitmap >> i) & 1) == 0 {
            continue;
        }
        acc += G2Projective::from(points[i]);
    }
    g2_jacobian_limbs_from_projective(&acc)
}
