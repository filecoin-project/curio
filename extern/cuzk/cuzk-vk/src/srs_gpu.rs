//! Phase I — SRS **G1 / G2 affine** staging in GPU Montgomery limb layout (upload path smoke).
//!
//! Uses `g1_reverse24` / `g2_reverse48` as deterministic permutation oracles: decode to
//! [`blstrs::G1Affine`] / [`blstrs::G2Affine`], convert to [`crate::g1::G1AffineLimbs`] /
//! [`crate::g1::G2AffineLimbs`], GPU reverse must match the CPU reference
//! ([`crate::g1::g1_limbs_reverse_words_inplace`], [`crate::g1::g2_limbs_reverse_words_inplace`]).

use anyhow::{ensure, Result};
use blstrs::{G1Affine, G2Affine};

use crate::device::VulkanDevice;
use crate::g1::{
    g1_affine_limbs_from_blstrs, g1_limbs_reverse_words_inplace, g2_affine_limbs_from_blstrs,
    g2_limbs_reverse_words_inplace,
};
use crate::g1_gpu::run_g1_reverse24_gpu;
use crate::g2_gpu::run_g2_reverse48_gpu;

/// GPU `g1_reverse24` on SRS-style Montgomery limbs must match the CPU bit-reversal reference.
pub fn srs_g1_affine_gpu_reverse24_matches_cpu(dev: &VulkanDevice, a: &G1Affine) -> Result<()> {
    let mut gpu = g1_affine_limbs_from_blstrs(a);
    let mut cpu = gpu;
    g1_limbs_reverse_words_inplace(&mut cpu);
    run_g1_reverse24_gpu(dev, &mut gpu)?;
    ensure!(
        gpu == cpu,
        "SRS G1 Montgomery limb GPU reverse mismatch (GPU vs CPU reference)"
    );
    Ok(())
}

/// GPU `g2_reverse48` on SRS-style Montgomery `Fp2` limbs must match the CPU bit-reversal reference.
pub fn srs_g2_affine_gpu_reverse48_matches_cpu(dev: &VulkanDevice, a: &G2Affine) -> Result<()> {
    let mut gpu = g2_affine_limbs_from_blstrs(a);
    let mut cpu = gpu;
    g2_limbs_reverse_words_inplace(&mut cpu);
    run_g2_reverse48_gpu(dev, &mut gpu)?;
    ensure!(
        gpu == cpu,
        "SRS G2 Montgomery limb GPU reverse mismatch (GPU vs CPU reference)"
    );
    Ok(())
}
