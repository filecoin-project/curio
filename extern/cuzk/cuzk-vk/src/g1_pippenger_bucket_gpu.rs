//! GPU **Pippenger bucket accumulate** — §8.1 full-width scalar MSM.
//!
//! [`run_g1_pippenger_window_gpu`] and [`run_g1_pippenger_window_gpu_batch_var`] load
//! **`g1_pippenger_bucket_acc_spec.spv`**: when **`glslangValidator`** succeeds in `build.rs`, SPIR-V
//! uses **`local_size_x_id = 0`** (§8.3 **C.3**) and the host passes [`G1_PIPP_LOCAL_X`](crate::ec::G1_PIPP_LOCAL_X);
//! otherwise the module is a copy of the **naga** build (fixed workgroup **256**). One GPU thread per
//! `(circuit, bucket)` walks packed affines whose digit matches. The shader matches the straight-line EFD
//! pattern of `g1_batch_accum_bitmap1636_tail.comp`.
//!
//! [`run_g1_pippenger_msm_gpu`] drives the full Pippenger multiexp: for each scalar window
//! extract unsigned digits, call the GPU bucket phase, then combine on the host via
//! [`crate::g1_msm_bucket::g1_combine_single_window_bucket_sums`] +
//! [`crate::g1_msm_bucket::g1_combine_pippenger_windows`].
//!
//! **Batch packing:** [`run_g1_pippenger_window_gpu_batch_var`] packs **`batch`** circuits with **per-circuit**
//! lengths into one SSBO: global digit/affine index `pi` runs `[circuit_start[c],
//! circuit_start[c+1])`. When **`batch * bucket_count`** or **`sum(n_c)`** exceeds shader
//! limits, [`run_g1_pippenger_msm_gpu_batch`] splits into multiple dispatches via
//! [`pippenger_plan_batch_tiles`].
//!
//! **§8.4 D.2** prototype: [`run_g1_pippenger_msm_gpu_batch_shared_bases`] — identical SRS / MSM bases
//! across circuits; Montgomery limbs are built **once** on the host. Each dispatch uses **indexed** affine
//! SSBO packing (`base_idx[π]` → shared pool of **`n`** rows) so host writes and GPU reads duplicate bases
//! **`O(batch)`** scalar-index columns instead of **`O(batch·n)`** full limb rows.

use std::ops::Range;
use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::{G1Affine, G1Projective, Scalar};
use group::Group;

use crate::device::VulkanDevice;
use crate::ec::{
    g1_affine_montgomery_limbs, g1_projective_from_jacobian_limbs, G1JacobianLimbs,
    G1_PIPP_LOCAL_X, G1_PIPP_MAX_BATCH_CIRCUITS, G1_PIPP_MAX_BUCKET_COUNT, G1_PIPP_MAX_N,
    G1_PIPP_OFF_AFFINE_MODE, G1_PIPP_OFF_AFF_POOL, G1_PIPP_OFF_BASE_IDX, G1_PIPP_OFF_BATCH,
    G1_PIPP_OFF_BUCKET_COUNT, G1_PIPP_OFF_BUCKET_JAC, G1_PIPP_OFF_CIRCUIT_START, G1_PIPP_OFF_DIG,
    G1_PIPP_OFF_POINT_TOTAL, G1_PIPP_OFF_RESERVED, G1_PIPP_OFF_UNIQUE_AFF_COUNT,
    G1_PIPP_CIRCUIT_PREFIX_SLOTS, G1_PIPP_SSBO_BYTES,
};
use crate::g1::BLS12_381_FP_U32_LIMBS;
use crate::g1_msm_bucket::{g1_combine_pippenger_windows, g1_combine_single_window_bucket_sums};
#[cfg(g1_pippenger_bucket_acc_spec)]
use crate::pipelines::spec_constant_u32_le;
use crate::split_msm::fr_scalar_unsigned_windows;
use crate::vk_oneshot;
#[cfg(g1_pippenger_bucket_acc_spec)]
use crate::vk_oneshot::ComputeShaderStageSpec;

/// Aligned buffer size: SSBO rounded up to 4 KiB page.
const BUF_SIZE: u64 = ((G1_PIPP_SSBO_BYTES as u64) + 4095) & !4095;

#[inline]
fn put_u32(buf: &mut [u8], word: usize, v: u32) {
    let o = word * 4;
    buf[o..o + 4].copy_from_slice(&v.to_le_bytes());
}

#[inline]
fn put_fp12(buf: &mut [u8], word: usize, limbs: &[u32; BLS12_381_FP_U32_LIMBS]) {
    for i in 0..BLS12_381_FP_U32_LIMBS {
        put_u32(buf, word + i, limbs[i]);
    }
}

#[inline]
fn get_fp12(buf: &[u8], word: usize) -> [u32; BLS12_381_FP_U32_LIMBS] {
    let mut out = [0u32; BLS12_381_FP_U32_LIMBS];
    for i in 0..BLS12_381_FP_U32_LIMBS {
        let o = (word + i) * 4;
        out[i] = u32::from_le_bytes(buf[o..o + 4].try_into().unwrap());
    }
    out
}

/// One MSM instance inside [`run_g1_pippenger_msm_gpu_batch`]; effective length is
/// `min(bases.len(), scalars.len())`.
#[derive(Clone, Copy, Debug)]
pub struct G1PippengerBatchItem<'a> {
    pub bases: &'a [G1Affine],
    pub scalars: &'a [Scalar],
}

/// Montgomery limbs for one affine G1 base (`x||y`), matching Pippenger SSBO affine packing.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct G1AffineMontgomeryFp12 {
    pub x: [u32; BLS12_381_FP_U32_LIMBS],
    pub y: [u32; BLS12_381_FP_U32_LIMBS],
}

impl G1AffineMontgomeryFp12 {
    #[inline]
    pub fn from_affine(affine: &G1Affine) -> Self {
        let (x, y) = g1_affine_montgomery_limbs(affine);
        Self { x, y }
    }
}

/// Montgomery table for **SRS / MSM bases** — compute once, reuse across circuits (**§8.4 D.2** host path).
#[inline]
pub fn g1_affine_montgomery_fp12_table(bases: &[G1Affine]) -> Vec<G1AffineMontgomeryFp12> {
    bases.iter().map(G1AffineMontgomeryFp12::from_affine).collect()
}

#[inline]
fn pippenger_dispatch_groups_x(batch: u32, bucket_count: usize) -> u32 {
    let jobs = (batch as u64).saturating_mul(bucket_count as u64);
    jobs.div_ceil(G1_PIPP_LOCAL_X as u64) as u32
}

/// Allowed [`G1_PIPP_LOCAL_X`](crate::ec::G1_PIPP_LOCAL_X) for dispatch math matches
/// [`crate::msm_gpu::MSM_DISPATCH_SMOKE_LOCAL_X_MIN`]..=[`crate::msm_gpu::MSM_DISPATCH_SMOKE_LOCAL_X_MAX`]
/// when SPIR-V uses **`local_size_x_id = 0`** (**§8.3 C.3**).
pub const G1_PIPP_LOCAL_X_SPEC_MIN: u32 = 1;
/// Upper bound for specialized Pippenger workgroup X (Vulkan typical max).
pub const G1_PIPP_LOCAL_X_SPEC_MAX: u32 = 256;

/// Partition global circuit indices into tiles so each tile fits one SSBO/dispatch:
/// `sum(n)` ≤ [`G1_PIPP_MAX_N`], `tile_len * bucket_count` ≤ [`G1_PIPP_MAX_BUCKET_COUNT`],
/// `tile_len` ≤ [`G1_PIPP_MAX_BATCH_CIRCUITS`].
pub fn pippenger_plan_batch_tiles(ns: &[usize], bucket_count: usize) -> Result<Vec<Range<usize>>> {
    anyhow::ensure!(
        bucket_count >= 2 && bucket_count <= G1_PIPP_MAX_BUCKET_COUNT,
        "bucket_count={bucket_count} out of [2, G1_PIPP_MAX_BUCKET_COUNT]"
    );
    let mut tiles = Vec::new();
    let mut i = 0usize;
    while i < ns.len() {
        let mut j = i;
        let mut sum_pts = 0usize;
        while j < ns.len() {
            let nc = ns[j];
            anyhow::ensure!(
                nc > 0 && nc <= G1_PIPP_MAX_N,
                "circuit n={nc} must be in 1..={G1_PIPP_MAX_N}"
            );
            let tile_len = j - i + 1;
            let new_sum = sum_pts.saturating_add(nc);
            if new_sum > G1_PIPP_MAX_N {
                break;
            }
            if tile_len > G1_PIPP_MAX_BATCH_CIRCUITS {
                break;
            }
            if tile_len.saturating_mul(bucket_count) > G1_PIPP_MAX_BUCKET_COUNT {
                break;
            }
            sum_pts = new_sum;
            j += 1;
        }
        anyhow::ensure!(
            j > i,
            "circuit at index {i} cannot fit Pippenger SSBO limits (n={}, bucket_count={bucket_count})",
            ns[i],
        );
        tiles.push(i..j);
        i = j;
    }
    Ok(tiles)
}

/// `circuit_start[c]` = cumulative points before circuit `c`; length **`batch + 1`**,
/// last entry = total packed points (≤ [`G1_PIPP_MAX_N`]).
pub fn circuit_start_prefix_u32(ns: &[usize]) -> Result<Vec<u32>> {
    let mut out = Vec::with_capacity(ns.len() + 1);
    let mut acc = 0u32;
    out.push(0);
    for &nc in ns {
        let add = u32::try_from(nc).map_err(|_| anyhow::anyhow!("n overflow u32"))?;
        acc = acc
            .checked_add(add)
            .ok_or_else(|| anyhow::anyhow!("circuit_start cumulative overflow"))?;
        anyhow::ensure!(
            (acc as usize) <= G1_PIPP_MAX_N,
            "circuit prefix total {acc} exceeds G1_PIPP_MAX_N={G1_PIPP_MAX_N}"
        );
        out.push(acc);
    }
    Ok(out)
}

fn validate_circuit_starts(circuit_starts: &[u32], bucket_count: usize) -> Result<(u32, u32)> {
    anyhow::ensure!(
        circuit_starts.len() >= 2,
        "circuit_starts must have batch+1 entries (at least one circuit)"
    );
    anyhow::ensure!(
        circuit_starts.len() <= G1_PIPP_CIRCUIT_PREFIX_SLOTS,
        "batch {} exceeds G1_PIPP_MAX_BATCH_CIRCUITS={}",
        circuit_starts.len() - 1,
        G1_PIPP_MAX_BATCH_CIRCUITS,
    );
    anyhow::ensure!(
        circuit_starts[0] == 0,
        "circuit_starts[0] must be 0"
    );
    let batch = (circuit_starts.len() - 1) as u32;
    let n_tot = *circuit_starts.last().expect("len>=2");
    anyhow::ensure!(
        (n_tot as usize) <= G1_PIPP_MAX_N,
        "N_tot={n_tot} exceeds G1_PIPP_MAX_N={G1_PIPP_MAX_N}"
    );
    for k in 0..circuit_starts.len() - 1 {
        anyhow::ensure!(
            circuit_starts[k] <= circuit_starts[k + 1],
            "circuit_start must be non-decreasing"
        );
    }
    anyhow::ensure!(
        (batch as usize).saturating_mul(bucket_count) <= G1_PIPP_MAX_BUCKET_COUNT,
        "batch*bucket_count ({}*{bucket_count}) exceeds G1_PIPP_MAX_BUCKET_COUNT={}",
        batch,
        G1_PIPP_MAX_BUCKET_COUNT,
    );
    anyhow::ensure!(
        bucket_count >= 2 && bucket_count <= G1_PIPP_MAX_BUCKET_COUNT,
        "bucket_count={bucket_count} out of range"
    );
    Ok((batch, n_tot))
}

#[derive(Clone, Copy)]
enum PippengerAffineSsbo<'a> {
    Dense(&'a [G1AffineMontgomeryFp12]),
    Indexed {
        pool: &'a [G1AffineMontgomeryFp12],
        base_idx: &'a [u32],
    },
}

fn write_pippenger_ssbo(
    wbytes: &mut [u8],
    circuit_starts: &[u32],
    bucket_count: usize,
    batch_circuits: u32,
    n_tot: u32,
    digits_all: &[u32],
    aff: PippengerAffineSsbo<'_>,
) -> Result<()> {
    let n_tot_us = n_tot as usize;
    anyhow::ensure!(
        digits_all.len() >= n_tot_us,
        "digits shorter than N_tot ({n_tot_us})"
    );

    put_u32(wbytes, G1_PIPP_OFF_RESERVED, 0);
    put_u32(wbytes, G1_PIPP_OFF_BUCKET_COUNT, bucket_count as u32);
    put_u32(wbytes, G1_PIPP_OFF_BATCH, batch_circuits);
    put_u32(wbytes, G1_PIPP_OFF_POINT_TOTAL, n_tot);

    let (affine_mode, unique_aff) = match aff {
        PippengerAffineSsbo::Dense(limbs) => {
            anyhow::ensure!(
                limbs.len() >= n_tot_us,
                "affine_limbs shorter than N_tot ({n_tot_us})"
            );
            (0u32, n_tot)
        }
        PippengerAffineSsbo::Indexed { pool, base_idx } => {
            anyhow::ensure!(
                base_idx.len() >= n_tot_us,
                "base_idx shorter than N_tot ({n_tot_us})"
            );
            for pi in 0..n_tot_us {
                let ix = base_idx[pi] as usize;
                anyhow::ensure!(
                    ix < pool.len(),
                    "base_idx[{pi}]={ix} out of range for pool len {}",
                    pool.len()
                );
            }
            (1u32, pool.len() as u32)
        }
    };
    put_u32(wbytes, G1_PIPP_OFF_AFFINE_MODE, affine_mode);
    put_u32(wbytes, G1_PIPP_OFF_UNIQUE_AFF_COUNT, unique_aff);

    for slot in 0..G1_PIPP_CIRCUIT_PREFIX_SLOTS {
        let v = circuit_starts.get(slot).copied().unwrap_or(0);
        put_u32(wbytes, G1_PIPP_OFF_CIRCUIT_START + slot, v);
    }

    for idx in 0..n_tot_us {
        put_u32(wbytes, G1_PIPP_OFF_DIG + idx, digits_all[idx]);
    }

    match aff {
        PippengerAffineSsbo::Dense(limbs) => {
            for idx in 0..n_tot_us {
                let lm = limbs[idx];
                let off = G1_PIPP_OFF_AFF_POOL + idx * 2 * BLS12_381_FP_U32_LIMBS;
                put_fp12(wbytes, off, &lm.x);
                put_fp12(wbytes, off + BLS12_381_FP_U32_LIMBS, &lm.y);
            }
        }
        PippengerAffineSsbo::Indexed { pool, base_idx } => {
            for pi in 0..n_tot_us {
                put_u32(wbytes, G1_PIPP_OFF_BASE_IDX + pi, base_idx[pi]);
            }
            for j in 0..pool.len() {
                let lm = pool[j];
                let off = G1_PIPP_OFF_AFF_POOL + j * 2 * BLS12_381_FP_U32_LIMBS;
                put_fp12(wbytes, off, &lm.x);
                put_fp12(wbytes, off + BLS12_381_FP_U32_LIMBS, &lm.y);
            }
        }
    }

    Ok(())
}

fn read_pippenger_bucket_jacobians(
    rbytes: &[u8],
    batch: usize,
    bucket_count: usize,
) -> Vec<Vec<G1JacobianLimbs>> {
    let jac_words = 3 * BLS12_381_FP_U32_LIMBS;
    let mut per_circuit = Vec::with_capacity(batch);
    for c in 0..batch {
        let mut buckets = Vec::with_capacity(bucket_count);
        for b in 0..bucket_count {
            let lin = c * bucket_count + b;
            let off = G1_PIPP_OFF_BUCKET_JAC + lin * jac_words;
            buckets.push(G1JacobianLimbs {
                x: get_fp12(rbytes, off),
                y: get_fp12(rbytes, off + 12),
                z: get_fp12(rbytes, off + 24),
            });
        }
        per_circuit.push(buckets);
    }
    per_circuit
}

fn run_g1_pippenger_window_gpu_batch_var_any(
    dev: &VulkanDevice,
    circuit_starts: &[u32],
    bucket_count: usize,
    digits_all: &[u32],
    aff: PippengerAffineSsbo<'_>,
) -> Result<Vec<Vec<G1JacobianLimbs>>> {
    let (batch_circuits, n_tot) = validate_circuit_starts(circuit_starts, bucket_count)?;
    anyhow::ensure!(
        (G1_PIPP_LOCAL_X_SPEC_MIN..=G1_PIPP_LOCAL_X_SPEC_MAX).contains(&G1_PIPP_LOCAL_X),
        "G1_PIPP_LOCAL_X={} must lie in {}..={} for Pippenger dispatch / specialization",
        G1_PIPP_LOCAL_X,
        G1_PIPP_LOCAL_X_SPEC_MIN,
        G1_PIPP_LOCAL_X_SPEC_MAX,
    );

    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_pippenger_bucket_acc_spec.spv"));
    let spirv_words = read_spv(&mut Cursor::new(spirv.as_slice()))
        .context("read_spv g1_pippenger_bucket_acc_spec")?;

    let mut wbytes = vec![0u8; G1_PIPP_SSBO_BYTES];
    write_pippenger_ssbo(
        &mut wbytes,
        circuit_starts,
        bucket_count,
        batch_circuits,
        n_tot,
        digits_all,
        aff,
    )?;

    let mut rbytes = vec![0u8; G1_PIPP_SSBO_BYTES];
    let groups_x = pippenger_dispatch_groups_x(batch_circuits, bucket_count);

    #[cfg(g1_pippenger_bucket_acc_spec)]
    let lx_le = spec_constant_u32_le(G1_PIPP_LOCAL_X);
    #[cfg(g1_pippenger_bucket_acc_spec)]
    let map = [vk_oneshot::spec_map_u32(0, 0)];
    #[cfg(g1_pippenger_bucket_acc_spec)]
    let stage_spec = ComputeShaderStageSpec {
        map_entries: &map,
        data: lx_le.as_slice(),
    };

    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            BUF_SIZE,
            BUF_SIZE,
            (groups_x, 1, 1),
            &wbytes,
            G1_PIPP_SSBO_BYTES,
            &mut rbytes,
            #[cfg(g1_pippenger_bucket_acc_spec)]
            Some(&stage_spec),
            #[cfg(not(g1_pippenger_bucket_acc_spec))]
            None,
        )?;
    }

    Ok(read_pippenger_bucket_jacobians(
        &rbytes,
        batch_circuits as usize,
        bucket_count,
    ))
}

/// Batched Pippenger **bucket phase** with **variable** per-circuit lengths.
///
/// `circuit_starts.len() == batch + 1`; packed order matches increasing `pi` over circuits.
pub fn run_g1_pippenger_window_gpu_batch_var(
    dev: &VulkanDevice,
    circuit_starts: &[u32],
    bucket_count: usize,
    bases_all: &[G1Affine],
    digits_all: &[u32],
) -> Result<Vec<Vec<G1JacobianLimbs>>> {
    let (_, n_tot) = validate_circuit_starts(circuit_starts, bucket_count)?;
    let n_tot_us = n_tot as usize;
    anyhow::ensure!(
        bases_all.len() >= n_tot_us && digits_all.len() >= n_tot_us,
        "bases/digits shorter than N_tot ({n_tot_us})"
    );
    let limbs: Vec<G1AffineMontgomeryFp12> = bases_all[..n_tot_us]
        .iter()
        .map(G1AffineMontgomeryFp12::from_affine)
        .collect();
    run_g1_pippenger_window_gpu_batch_var_limbs(dev, circuit_starts, bucket_count, &limbs, digits_all)
}

/// Same as [`run_g1_pippenger_window_gpu_batch_var`], but affine SSBO rows come from pre-packed Montgomery
/// limbs (§8.4 **D.2** host reuse). Uses **dense** layout: pool row **`π`** holds the affine for logical row **`π`**.
pub fn run_g1_pippenger_window_gpu_batch_var_limbs(
    dev: &VulkanDevice,
    circuit_starts: &[u32],
    bucket_count: usize,
    affine_limbs: &[G1AffineMontgomeryFp12],
    digits_all: &[u32],
) -> Result<Vec<Vec<G1JacobianLimbs>>> {
    run_g1_pippenger_window_gpu_batch_var_any(
        dev,
        circuit_starts,
        bucket_count,
        digits_all,
        PippengerAffineSsbo::Dense(affine_limbs),
    )
}

/// Batched bucket phase — **uniform** `n` per circuit (prefix `start[c] = c * n`).
pub fn run_g1_pippenger_window_gpu_batch(
    dev: &VulkanDevice,
    batch_circuits: u32,
    n: usize,
    bucket_count: usize,
    bases_all: &[G1Affine],
    digits_all: &[u32],
) -> Result<Vec<Vec<G1JacobianLimbs>>> {
    let batch = batch_circuits as usize;
    anyhow::ensure!(batch_circuits >= 1, "batch_circuits must be >= 1");
    anyhow::ensure!(
        n > 0 && n <= G1_PIPP_MAX_N,
        "n={n} exceeds G1_PIPP_MAX_N={G1_PIPP_MAX_N}"
    );
    let total_pts = batch.saturating_mul(n);
    anyhow::ensure!(
        total_pts <= G1_PIPP_MAX_N,
        "batch*n ({batch}*{n}={total_pts}) exceeds G1_PIPP_MAX_N={G1_PIPP_MAX_N}"
    );
    anyhow::ensure!(
        batch.saturating_mul(bucket_count) <= G1_PIPP_MAX_BUCKET_COUNT,
        "batch*bucket_count ({batch}*{bucket_count}) exceeds G1_PIPP_MAX_BUCKET_COUNT={G1_PIPP_MAX_BUCKET_COUNT}"
    );
    anyhow::ensure!(
        bases_all.len() >= total_pts && digits_all.len() >= total_pts,
        "bases/digits length < batch*n ({total_pts})"
    );

    let mut starts = vec![0u32; batch + 1];
    for c in 1..=batch {
        starts[c] = (c * n) as u32;
    }
    run_g1_pippenger_window_gpu_batch_var(dev, &starts, bucket_count, bases_all, digits_all)
}

/// Run one Pippenger window on the GPU (`batch_circuits = 1`).
///
/// `bases[0..n]` are affine G1 points (BLS12-381), `digits[0..n]` are the unsigned window
/// digits in `0..bucket_count` for this window. Returns a `Vec<G1JacobianLimbs>` of length
/// `bucket_count` where entry `b` holds `Σ_{i: digits[i]==b} bases[i]` in Jacobian form.
///
/// Requires `n ≤ G1_PIPP_MAX_N` and `2 ≤ bucket_count ≤ G1_PIPP_MAX_BUCKET_COUNT`.
pub fn run_g1_pippenger_window_gpu(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    digits: &[u32],
    n: usize,
    bucket_count: usize,
) -> Result<Vec<G1JacobianLimbs>> {
    let mut v = run_g1_pippenger_window_gpu_batch(dev, 1, n, bucket_count, bases, digits)?;
    Ok(v.pop().expect("one circuit"))
}

/// Full 255-bit scalar Pippenger MSM on the GPU.
///
/// Uses unsigned `window_bits`-wide windows over the 255 canonical scalar bits (LSB-first).
/// For each window: extracts digits, dispatches [`run_g1_pippenger_window_gpu`], combines
/// bucket sums on the host with [`g1_combine_single_window_bucket_sums`]. Final result is
/// assembled via [`g1_combine_pippenger_windows`] (Horner: `Σ_w result_w · 2^(w·W)`).
///
/// Requires `n ≤ G1_PIPP_MAX_N` and `1 ≤ window_bits ≤ 16`.
pub fn run_g1_pippenger_msm_gpu(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    scalars: &[Scalar],
    n: usize,
    window_bits: u32,
) -> Result<G1Projective> {
    anyhow::ensure!(
        n > 0 && n <= G1_PIPP_MAX_N,
        "n={n} out of range for Pippenger MSM"
    );
    anyhow::ensure!(
        window_bits >= 1 && window_bits <= 16,
        "window_bits={window_bits}"
    );
    anyhow::ensure!(bases.len() >= n && scalars.len() >= n);

    let bucket_count = 1usize << window_bits;
    let num_windows = (255u32).div_ceil(window_bits) as usize;

    // Pre-compute per-scalar window digits once.
    let all_digits: Vec<Vec<u32>> = scalars[..n]
        .iter()
        .map(|s| fr_scalar_unsigned_windows(s, window_bits))
        .collect();

    let mut window_results: Vec<G1Projective> = Vec::with_capacity(num_windows);

    for w in 0..num_windows {
        let digits: Vec<u32> = (0..n).map(|i| all_digits[i][w]).collect();
        let bucket_jac = run_g1_pippenger_window_gpu(dev, &bases[..n], &digits, n, bucket_count)
            .with_context(|| format!("pippenger window {w}/{num_windows}"))?;

        // Convert Jacobian buckets → projective, combine: Σ_{b=1}^{bc-1} b·bucket[b].
        let bucket_proj: Vec<G1Projective> = bucket_jac
            .iter()
            .map(g1_projective_from_jacobian_limbs)
            .collect();
        let wr = g1_combine_single_window_bucket_sums(window_bits, &bucket_proj)
            .ok_or_else(|| anyhow::anyhow!("g1_combine_single_window_bucket_sums window {w}"))?;
        window_results.push(wr);
    }

    // Assemble across windows: result = Σ_w window[w] · 2^(w·window_bits).
    Ok(g1_combine_pippenger_windows(window_bits, &window_results))
}

/// CPU reference Pippenger MSM — same windowing as the GPU path, fully on the host.
/// Used to verify correctness of [`run_g1_pippenger_msm_gpu`].
pub fn g1_pippenger_msm_cpu(
    bases: &[G1Affine],
    scalars: &[Scalar],
    n: usize,
    window_bits: u32,
) -> G1Projective {
    assert!(n > 0 && bases.len() >= n && scalars.len() >= n);
    assert!(window_bits >= 1 && window_bits <= 16);

    let bucket_count = 1usize << window_bits;
    let num_windows = (255u32).div_ceil(window_bits) as usize;
    let all_digits: Vec<Vec<u32>> = scalars[..n]
        .iter()
        .map(|s| fr_scalar_unsigned_windows(s, window_bits))
        .collect();

    let mut window_results = Vec::with_capacity(num_windows);
    for w in 0..num_windows {
        let mut buckets = vec![G1Projective::identity(); bucket_count];
        for i in 0..n {
            let b = all_digits[i][w] as usize;
            if b < bucket_count {
                buckets[b] += G1Projective::from(bases[i]);
            }
        }
        let wr = g1_combine_single_window_bucket_sums(window_bits, &buckets)
            .unwrap_or(G1Projective::identity());
        window_results.push(wr);
    }
    g1_combine_pippenger_windows(window_bits, &window_results)
}

/// Full 255-bit Pippenger MSM — batched independent circuits (variable length per circuit).
///
/// Each scalar window: one or more [`run_g1_pippenger_window_gpu_batch_var`] dispatches
/// according to [`pippenger_plan_batch_tiles`].
pub fn run_g1_pippenger_msm_gpu_batch(
    dev: &VulkanDevice,
    circuits: &[G1PippengerBatchItem<'_>],
    window_bits: u32,
) -> Result<Vec<G1Projective>> {
    let batch = circuits.len();
    anyhow::ensure!(batch >= 1, "circuits batch must be non-empty");
    anyhow::ensure!(
        window_bits >= 1 && window_bits <= 16,
        "window_bits={window_bits}"
    );

    let ns: Vec<usize> = circuits
        .iter()
        .map(|c| c.bases.len().min(c.scalars.len()))
        .collect();
    for (idx, &nc) in ns.iter().enumerate() {
        anyhow::ensure!(
            nc > 0,
            "circuit {idx} has empty bases/scalars (effective n=0)"
        );
    }

    let bucket_count = 1usize << window_bits;
    let tiles = pippenger_plan_batch_tiles(&ns, bucket_count)?;

    let num_windows = (255u32).div_ceil(window_bits) as usize;

    let digit_tables: Vec<Vec<Vec<u32>>> = circuits
        .iter()
        .enumerate()
        .map(|(ci, c)| {
            (0..ns[ci])
                .map(|i| fr_scalar_unsigned_windows(&c.scalars[i], window_bits))
                .collect()
        })
        .collect();

    let mut window_accum: Vec<Vec<G1Projective>> = vec![Vec::with_capacity(num_windows); batch];

    for w in 0..num_windows {
        for tile in &tiles {
            let local_ns = &ns[tile.clone()];
            let starts = circuit_start_prefix_u32(local_ns)?;
            let n_tot = *starts.last().expect("prefix") as usize;

            let mut bases_concat = Vec::with_capacity(n_tot);
            let mut digits_concat = Vec::with_capacity(n_tot);
            for global_c in tile.clone() {
                let nc = ns[global_c];
                for i in 0..nc {
                    bases_concat.push(circuits[global_c].bases[i]);
                    digits_concat.push(digit_tables[global_c][i][w]);
                }
            }

            let bucket_batch = run_g1_pippenger_window_gpu_batch_var(
                dev,
                &starts,
                bucket_count,
                &bases_concat,
                &digits_concat,
            )
            .with_context(|| format!("pippenger batch window {w}/{num_windows} tile {tile:?}"))?;

            for (local_idx, bucket_jac) in bucket_batch.iter().enumerate() {
                let global_c = tile.start + local_idx;
                let bucket_proj: Vec<G1Projective> = bucket_jac
                    .iter()
                    .map(g1_projective_from_jacobian_limbs)
                    .collect();
                let wr = g1_combine_single_window_bucket_sums(window_bits, &bucket_proj).ok_or_else(|| {
                    anyhow::anyhow!(
                        "g1_combine_single_window_bucket_sums batch window {w} global circuit {global_c}"
                    )
                })?;
                window_accum[global_c].push(wr);
            }
        }
    }

    Ok(window_accum
        .into_iter()
        .map(|wr| g1_combine_pippenger_windows(window_bits, &wr))
        .collect())
}

/// §8.4 **D.2** prototype — multiple **full-width** MSMs over the **same** affine bases (SRS `h[]`, fixed table).
///
/// Montgomery limbs for `bases` are computed **once** ([`g1_affine_montgomery_fp12_table`]) and reused for every
/// window/tile; scalar digits still vary per circuit. Affine Montgomery **`x‖y`** rows are uploaded **`n`** times
/// per dispatch (shared pool) instead of **`N_tot`**, with `base_idx[π]=π mod n` (**indexed** SSBO layout).
pub fn run_g1_pippenger_msm_gpu_batch_shared_bases(
    dev: &VulkanDevice,
    bases: &[G1Affine],
    scalars_rows: &[&[Scalar]],
    window_bits: u32,
) -> Result<Vec<G1Projective>> {
    let batch = scalars_rows.len();
    anyhow::ensure!(batch >= 1, "shared-bases batch must be non-empty");
    anyhow::ensure!(
        window_bits >= 1 && window_bits <= 16,
        "window_bits={window_bits}"
    );
    let n = bases.len();
    anyhow::ensure!(
        n > 0 && n <= G1_PIPP_MAX_N,
        "bases.len()={n} invalid for Pippenger"
    );
    for (idx, row) in scalars_rows.iter().enumerate() {
        anyhow::ensure!(
            row.len() >= n,
            "scalars_rows[{idx}] len {} < bases.len() ({n})",
            row.len(),
        );
    }

    let limb_table = g1_affine_montgomery_fp12_table(bases);

    let ns = vec![n; batch];
    let bucket_count = 1usize << window_bits;
    let tiles = pippenger_plan_batch_tiles(&ns, bucket_count)?;

    let num_windows = (255u32).div_ceil(window_bits) as usize;

    let digit_tables: Vec<Vec<Vec<u32>>> = scalars_rows
        .iter()
        .map(|row| {
            (0..n)
                .map(|i| fr_scalar_unsigned_windows(&row[i], window_bits))
                .collect()
        })
        .collect();

    let mut window_accum: Vec<Vec<G1Projective>> = vec![Vec::with_capacity(num_windows); batch];

    for w in 0..num_windows {
        for tile in &tiles {
            let local_ns = &ns[tile.clone()];
            let starts = circuit_start_prefix_u32(local_ns)?;
            let n_tot = *starts.last().expect("prefix") as usize;

            let mut base_idx = Vec::with_capacity(n_tot);
            let mut digits_concat = Vec::with_capacity(n_tot);
            for global_c in tile.clone() {
                for i in 0..n {
                    base_idx.push(i as u32);
                    digits_concat.push(digit_tables[global_c][i][w]);
                }
            }

            let bucket_batch = run_g1_pippenger_window_gpu_batch_var_any(
                dev,
                &starts,
                bucket_count,
                &digits_concat,
                PippengerAffineSsbo::Indexed {
                    pool: limb_table.as_slice(),
                    base_idx: base_idx.as_slice(),
                },
            )
            .with_context(|| {
                format!("shared-bases Pippenger window {w}/{num_windows} tile {tile:?}")
            })?;

            for (local_idx, bucket_jac) in bucket_batch.iter().enumerate() {
                let global_c = tile.start + local_idx;
                let bucket_proj: Vec<G1Projective> = bucket_jac
                    .iter()
                    .map(g1_projective_from_jacobian_limbs)
                    .collect();
                let wr = g1_combine_single_window_bucket_sums(window_bits, &bucket_proj).ok_or_else(|| {
                    anyhow::anyhow!(
                        "g1_combine_single_window_bucket_sums shared-bases window {w} circuit {global_c}"
                    )
                })?;
                window_accum[global_c].push(wr);
            }
        }
    }

    Ok(window_accum
        .into_iter()
        .map(|wr| g1_combine_pippenger_windows(window_bits, &wr))
        .collect())
}

/// CPU batched MSM oracle ([`run_g1_pippenger_msm_gpu_batch`]).
pub fn g1_pippenger_msm_cpu_batch(
    circuits: &[G1PippengerBatchItem<'_>],
    window_bits: u32,
) -> Vec<G1Projective> {
    circuits
        .iter()
        .map(|c| {
            let n = c.bases.len().min(c.scalars.len());
            assert!(n > 0, "empty circuit in cpu_batch");
            g1_pippenger_msm_cpu(c.bases, c.scalars, n, window_bits)
        })
        .collect()
}

/// CPU oracle for [`run_g1_pippenger_msm_gpu_batch_shared_bases`].
pub fn g1_pippenger_msm_cpu_batch_shared_bases(
    bases: &[G1Affine],
    scalars_rows: &[&[Scalar]],
    window_bits: u32,
) -> Vec<G1Projective> {
    scalars_rows
        .iter()
        .map(|row| {
            let n = bases.len().min(row.len());
            assert!(n > 0, "empty scalar row in cpu_batch_shared_bases");
            g1_pippenger_msm_cpu(bases, row, n, window_bits)
        })
        .collect()
}

#[cfg(test)]
mod tiling_tests {
    use super::*;
    use group::{Curve, Group};

    #[test]
    fn cpu_shared_bases_matches_duplicate_bases_batch() {
        let wb = 8u32;
        let n = 8usize;
        let bases: Vec<G1Affine> = (0..n)
            .map(|i| (G1Projective::generator() * Scalar::from(i as u64 + 5)).to_affine())
            .collect();
        let s0: Vec<Scalar> = (0..n).map(|i| Scalar::from(i as u64 + 101)).collect();
        let s1: Vec<Scalar> = (0..n).map(|i| Scalar::from(i as u64 + 701)).collect();
        let circuits = [
            G1PippengerBatchItem {
                bases: bases.as_slice(),
                scalars: s0.as_slice(),
            },
            G1PippengerBatchItem {
                bases: bases.as_slice(),
                scalars: s1.as_slice(),
            },
        ];
        let want = g1_pippenger_msm_cpu_batch(&circuits, wb);
        let got = g1_pippenger_msm_cpu_batch_shared_bases(&bases, &[s0.as_slice(), s1.as_slice()], wb);
        assert_eq!(got, want);
    }

    #[test]
    fn plan_uniform_n_respects_max_batch() {
        let n = 16usize;
        let bc = 256usize;
        let ns = vec![n; 129];
        let tiles = pippenger_plan_batch_tiles(&ns, bc).unwrap();
        assert!(tiles.len() >= 2);
        assert_eq!(tiles[0].len(), 128);
        assert_eq!(tiles[1].len(), 1);
    }

    #[test]
    fn plan_w16_one_per_tile() {
        let bc = 65536usize;
        let ns = [8usize, 8];
        let tiles = pippenger_plan_batch_tiles(&ns, bc).unwrap();
        assert_eq!(tiles.len(), 2);
        assert_eq!(tiles[0], 0..1);
        assert_eq!(tiles[1], 1..2);
    }

    #[test]
    fn circuit_prefix_cumulative() {
        let ns = [3usize, 10, 5];
        let p = circuit_start_prefix_u32(&ns).unwrap();
        assert_eq!(p, vec![0u32, 3, 13, 18]);
    }
}
