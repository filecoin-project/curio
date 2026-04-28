//! Phase I — SRS **G1 affine** staging in GPU Montgomery limb layout (upload path smoke).
//!
//! Uses the existing `g1_reverse24` compute shader as a deterministic permutation oracle: SRS
//! bytes decode to [`blstrs::G1Affine`], convert to [`crate::g1::G1AffineLimbs`], GPU reverse must
//! match the CPU reference ([`crate::g1::g1_limbs_reverse_words_inplace`]).

use anyhow::{ensure, Result};
use blstrs::G1Affine;

use crate::device::VulkanDevice;
use crate::g1::{g1_affine_limbs_from_blstrs, g1_limbs_reverse_words_inplace};
use crate::g1_gpu::run_g1_reverse24_gpu;

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
