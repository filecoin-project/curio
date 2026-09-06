//! Power-of-two Fr NTT for `n <= 2^14` on the GPU (Montgomery `u32[8]`, multi-dispatch).
//!
//! Matches [`crate::ntt::fr_ntt_inplace`] / [`crate::ntt::fr_intt_inplace`] with [`crate::ntt::fr_omega`].
//! §8.2 **B.5:** [`run_fr_ntt_general_forward_gpu_distribute`] / [`run_fr_ntt_radix8_forward_gpu_distribute`] fold `base^i` coefficient scaling into host SSBO packing (coset forward via [`crate::fr_coset_gpu::run_fr_coset_fft_forward_gpu`], radix-8 when `n ≥ 8`).

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;
use blstrs::Scalar;
use ff::Field;

use crate::device::VulkanDevice;
use crate::g1::BLS12_381_FR_U32_LIMBS;
use crate::ntt::FrNttPlan;
use crate::pipelines::spec_constant_u32_le;
use crate::scalar_limbs::{scalar_from_montgomery_u32_limbs, scalar_montgomery_u32_limbs};
use crate::vk_oneshot::{
    run_compute_passes_1x_storage_buffer, spec_map_u32, ComputePassDesc, ComputeShaderStageSpec,
};

/// §8.2 B.1 — dispatch counts for the radix-4 NTT.
///
/// Returns `(k4, k2)`:
/// - `k4 = log_n / 2` radix-4 stages (quarter = 4^i for i = 0..k4) run first.
/// - `k2 = log_n % 2` trailing radix-2 stage (0 or 1) runs last when `log_n` is odd.
///
/// Wlens use natural slot indexing matching the underlying DIT stage number `s`:
/// - Radix-4 stage `i` covers DIT stages `2i` and `2i+1`; wlen stored at slot `2*i+1`.
/// - Trailing radix-2 covers DIT stage `log_n-1`; wlen stored at slot `log_n-1`.
///
/// The radix-2 tail shader loads `wlen^(j_in)` from the SSBO twiddle blob (`§8.2` **B.4**); `wlen`
/// for that stage stays at slot `log_n-1` so the host table matches the Butterflies.
#[inline]
pub const fn fr_ntt_radix4_stage_counts(log_n: u32) -> (u32, u32) {
    (log_n / 2, log_n % 2)
}

/// Total dispatch passes for forward radix-4 NTT: bitrev + copy + k4 + k2.
#[inline]
pub fn fr_ntt_general_forward_pass_count_radix4(log_n: u32) -> u32 {
    let (k4, k2) = fr_ntt_radix4_stage_counts(log_n);
    2 + k4 + k2
}

/// Total dispatch passes for inverse radix-4 NTT: bitrev + copy + k4 + k2 + scale.
#[inline]
pub fn fr_ntt_general_inverse_pass_count_radix4(log_n: u32) -> u32 {
    fr_ntt_general_forward_pass_count_radix4(log_n) + 1
}

/// `buf.d` header words (matches other Fr SSBO helpers).
pub const FR_NTT_GENERAL_HEADER_WORDS: usize = 64;
/// Maximum supported length (must match GLSL `FR_NTT_MAX_N`).
pub const FR_NTT_GENERAL_MAX_N: usize = 16384;
/// Per-stage `wlen` slots reserved (must match `FR_NTT_MAX_LOG` in shaders).
pub const FR_NTT_GENERAL_MAX_LOG: usize = 32;

/// §8.2 B.4 — max `u32` words for packed per-stage twiddle tables (`wlen^j` Montgomery limbs).
///
/// Worst case over `log_n ≤ 14` (`n ≤ 16384`): radix-4 schedule at `log_n = 14`,
/// `Σ_{i=0}^{6} 4^i = (4^7 - 1) / 3 = 5461` Fr slots × 8 limbs (radix-8 schedules use fewer).
pub const FR_NTT_GENERAL_TWIDDLE_U32_LEN: usize = 5461 * 8;

/// Compute passes in [`run_fr_ntt_general_forward_gpu`] (radix-2: bitrev + copy + `log_n` half butterflies).
#[inline]
pub const fn fr_ntt_general_forward_pass_count_radix2(log_n: u32) -> u32 {
    2 + log_n
}

/// Compute passes in [`run_fr_ntt_general_inverse_gpu`] (forward-shaped stages + `scale_ninv`).
#[inline]
pub const fn fr_ntt_general_inverse_pass_count_radix2(log_n: u32) -> u32 {
    3 + log_n
}

const WG: u32 = 256;

/// Total `u32` words in the general-NTT SSBO (header + data + scratch + twiddle bases).
#[inline]
pub fn fr_ntt_general_buf_u32_len() -> usize {
    FR_NTT_GENERAL_HEADER_WORDS
        + 2 * FR_NTT_GENERAL_MAX_N * 8
        + FR_NTT_GENERAL_MAX_LOG * 8
        + FR_NTT_GENERAL_TWIDDLE_U32_LEN
}

#[inline]
pub fn fr_ntt_general_buf_bytes() -> usize {
    fr_ntt_general_buf_u32_len() * 4
}

#[inline]
fn off_data_w() -> usize {
    FR_NTT_GENERAL_HEADER_WORDS
}

#[inline]
fn off_wstep_w() -> usize {
    FR_NTT_GENERAL_HEADER_WORDS + 2 * FR_NTT_GENERAL_MAX_N * 8
}

/// Word index of the twiddle blob base (`OFF_TWIDDLE` in GLSL).
#[inline]
fn off_twiddle_w() -> usize {
    off_wstep_w() + FR_NTT_GENERAL_MAX_LOG * 8
}

fn append_fr_twiddle_powers(wlen: Scalar, count: u32, blob: &mut Vec<u32>) {
    for j in 0..count {
        let pow = wlen.pow_vartime([j as u64]);
        blob.extend(scalar_montgomery_u32_limbs(&pow));
    }
}

/// Twiddle tables for radix-4 (+ optional radix-2 tail). Offsets are **word** offsets from `OFF_TWIDDLE`.
fn twiddle_blob_radix4_stages(
    n: usize,
    log_n: u32,
    sched_wlens: &[Scalar],
) -> (Vec<u32>, Vec<u32>, Option<(u32, u32)>) {
    let (k4, k2) = fr_ntt_radix4_stage_counts(log_n);
    let mut blob = Vec::new();
    let mut radix4_offs = Vec::with_capacity(k4 as usize);
    for i in 0..k4 {
        radix4_offs.push(blob.len() as u32);
        let quarter = 4u32.pow(i);
        let slot = 2 * i + 1;
        append_fr_twiddle_powers(sched_wlens[slot as usize], quarter, &mut blob);
    }
    let radix2 = if k2 == 1 {
        let off = blob.len() as u32;
        let stage = log_n - 1;
        let half = (n / 2) as u32;
        append_fr_twiddle_powers(sched_wlens[stage as usize], half, &mut blob);
        Some((off, stage))
    } else {
        None
    };
    debug_assert!(blob.len() <= FR_NTT_GENERAL_TWIDDLE_U32_LEN);
    (blob, radix4_offs, radix2)
}

/// Twiddle tables for radix-8 (+ optional radix-4 / radix-2 tail). Offsets from `OFF_TWIDDLE`.
fn twiddle_blob_radix8_stages(
    n: usize,
    log_n: u32,
    sched_wlens: &[Scalar],
    k8: u32,
    rem: u32,
) -> (Vec<u32>, Vec<u32>, Option<u32>, Option<u32>) {
    let mut blob = Vec::new();
    let mut radix8_offs = Vec::with_capacity(k8 as usize);
    for i in 0..k8 {
        radix8_offs.push(blob.len() as u32);
        let octet = 8u32.pow(i);
        let slot = 3 * i + 2;
        append_fr_twiddle_powers(sched_wlens[slot as usize], octet, &mut blob);
    }
    let mut radix4_tail_off = None;
    let mut radix2_tail_off = None;
    match rem {
        1 => {
            let off = blob.len() as u32;
            radix2_tail_off = Some(off);
            let half = (n / 2) as u32;
            append_fr_twiddle_powers(sched_wlens[(log_n - 1) as usize], half, &mut blob);
        }
        2 => {
            let off = blob.len() as u32;
            radix4_tail_off = Some(off);
            let quarter = 8u32.pow(k8);
            append_fr_twiddle_powers(sched_wlens[(log_n - 1) as usize], quarter, &mut blob);
        }
        _ => {}
    }
    debug_assert!(blob.len() <= FR_NTT_GENERAL_TWIDDLE_U32_LEN);
    (blob, radix8_offs, radix4_tail_off, radix2_tail_off)
}

fn push_zero() -> [u8; 16] {
    [0u8; 16]
}

/// Push constant for radix-4 / radix-8 stage shaders: `{quarter_or_octet, twiddle_word_off}` (§8.2 B.4).
fn push_radix_wide(quarter_or_octet: u32, twiddle_word_off: u32) -> [u8; 16] {
    let mut b = [0u8; 16];
    b[0..4].copy_from_slice(&quarter_or_octet.to_le_bytes());
    b[4..8].copy_from_slice(&twiddle_word_off.to_le_bytes());
    b
}

fn push_radix2_twiddle(stage: u32, twiddle_word_off: u32) -> [u8; 16] {
    let mut b = [0u8; 16];
    b[0..4].copy_from_slice(&stage.to_le_bytes());
    b[4..8].copy_from_slice(&twiddle_word_off.to_le_bytes());
    b
}

fn dispatch_for_quarter(n: u32) -> (u32, u32, u32) {
    (div_ceil_u32(n / 4, WG), 1, 1)
}

fn dispatch_for_octet(n: u32) -> (u32, u32, u32) {
    (div_ceil_u32(n / 8, WG), 1, 1)
}

/// Wlen table for the mixed radix-4 (+ optional trailing radix-2) NTT schedule.
///
/// Returns a Vec of length `log_n` indexed by natural DIT stage number `s`.
/// Most entries are unused (zero); only the slots actually referenced by the shaders are set:
///
/// - Radix-4 stage `i` (covering DIT stages `2i` and `2i+1`):
///   `wlen[2*i+1] = omega^(n / 4^(i+1))`
/// - Trailing radix-2 stage (odd `log_n` only):
///   `wlen[log_n-1] = omega^1`  (= `omega^(n / n)`)
///
/// Using natural slot indices avoids any remapping between Rust and the shaders.
fn radix4_schedule_wlens(n: usize, omega: Scalar, log_n: u32) -> Vec<Scalar> {
    let k4 = (log_n / 2) as usize;
    let odd = log_n % 2 == 1;
    let mut v = vec![Scalar::ZERO; log_n as usize];
    for i in 0..k4 {
        let slot = 2 * i + 1;
        let wlen = omega.pow_vartime([(n as u64) / 4u64.pow(i as u32 + 1)]);
        v[slot] = wlen;
    }
    if odd {
        // Trailing radix-2 covers DIT stage log_n-1 with half = n/2.
        // wlen = omega^(n / (2 * half)) = omega^1.
        let slot = (log_n - 1) as usize;
        let half = 1u64 << (log_n - 1);
        let wlen = omega.pow_vartime([(n as u64) / (2 * half)]);
        v[slot] = wlen;
    }
    v
}

/// §8.2 B.2 — dispatch counts for the radix-8 NTT.
///
/// Returns `(k8, remainder)`:
/// - `k8 = log_n / 3` radix-8 stages (octet = 8^i for i = 0..k8) run first.
/// - `remainder = log_n % 3`:
///   - 0: no trailing stage.
///   - 1: 1 trailing radix-2 stage (`twiddle` table for stage `log_n-1`, slot `log_n-1` = ω¹).
///   - 2: 1 trailing radix-4 stage (quarter = 8^k8, wlen_slot = log_n-1 = ω¹).
#[inline]
pub const fn fr_ntt_radix8_stage_counts(log_n: u32) -> (u32, u32) {
    (log_n / 3, log_n % 3)
}

/// Total dispatch passes for forward radix-8 NTT: bitrev + copy + k8 + (1 if remainder > 0).
#[inline]
pub fn fr_ntt_general_forward_pass_count_radix8(log_n: u32) -> u32 {
    let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
    2 + k8 + rem.min(1)
}

/// Total dispatch passes for inverse radix-8 NTT: forward-shaped + scale.
#[inline]
pub fn fr_ntt_general_inverse_pass_count_radix8(log_n: u32) -> u32 {
    fr_ntt_general_forward_pass_count_radix8(log_n) + 1
}

/// Wlen table for the radix-8 (+ optional radix-4/radix-2 tail) NTT schedule.
///
/// Returns a Vec of length `log_n` with natural DIT-stage slot indexing:
/// - Radix-8 stage `i` covers DIT stages `3i, 3i+1, 3i+2`; wlen at slot `3*i+2`:
///   `wlen[3*i+2] = omega^(n / 8^(i+1))`
/// - Trailing radix-4 (remainder=2) or trailing radix-2 (remainder=1): both use `ω¹`;
///   stored at slot `log_n-1` for host-built radix-tail twiddle tables (**§8.2 B.4**).
fn radix8_schedule_wlens(n: usize, omega: Scalar, log_n: u32) -> Vec<Scalar> {
    let k8 = (log_n / 3) as usize;
    let rem = log_n % 3;
    let mut v = vec![Scalar::ZERO; log_n as usize];
    for i in 0..k8 {
        let slot = 3 * i + 2;
        let wlen = omega.pow_vartime([(n as u64) / 8u64.pow(i as u32 + 1)]);
        v[slot] = wlen;
    }
    // Trailing r4 (rem=2) or r2 (rem=1): wlen = omega^1 = omega.
    if rem > 0 {
        v[(log_n - 1) as usize] = omega;
    }
    v
}

fn div_ceil_u32(a: u32, b: u32) -> u32 {
    (a + b - 1) / b
}

fn dispatch_for_n(n: u32) -> (u32, u32, u32) {
    (div_ceil_u32(n, WG), 1, 1)
}

fn dispatch_for_half(n: u32) -> (u32, u32, u32) {
    (div_ceil_u32(n / 2, WG), 1, 1)
}

/// Pack Fr NTT SSBO (`n`, `log_n`, coefficient limbs, stage wlens, optional `n_inv`, twiddle blob).
///
/// §8.2 **B.5:** when `distribute_base` is `Some(base)`, coefficients are packed as `coeffs[i] * base^i`
/// (same Fr algebra as [`crate::h_term::fr_distribute_powers`]), skipping a GPU pointwise dispatch before NTT.
fn pack_buf(
    n: usize,
    log_n: u32,
    coeffs: &[Scalar],
    stage_wlens_mont: &[[u32; BLS12_381_FR_U32_LIMBS]],
    n_inv_mont: Option<&[u32; BLS12_381_FR_U32_LIMBS]>,
    twiddle_mont: &[u32],
    distribute_base: Option<Scalar>,
) -> Vec<u8> {
    debug_assert!(twiddle_mont.len() <= FR_NTT_GENERAL_TWIDDLE_U32_LEN);
    let mut words = vec![0u32; fr_ntt_general_buf_u32_len()];
    words[0] = n as u32;
    words[1] = log_n;
    if let Some(inv) = n_inv_mont {
        for i in 0..BLS12_381_FR_U32_LIMBS {
            words[8 + i] = inv[i];
        }
    }
    let off_d = off_data_w();
    let mut gp_opt = distribute_base.map(|b| (Scalar::ONE, b));
    for (i, s) in coeffs.iter().enumerate().take(n) {
        let field_val = if let Some((ref mut gp, base)) = gp_opt {
            let v = *s * *gp;
            *gp *= base;
            v
        } else {
            *s
        };
        let m = scalar_montgomery_u32_limbs(&field_val);
        let base = off_d + i * BLS12_381_FR_U32_LIMBS;
        for j in 0..BLS12_381_FR_U32_LIMBS {
            words[base + j] = m[j];
        }
    }
    let off_w = off_wstep_w();
    for (si, wl) in stage_wlens_mont.iter().enumerate().take(log_n as usize) {
        let base = off_w + si * BLS12_381_FR_U32_LIMBS;
        for j in 0..BLS12_381_FR_U32_LIMBS {
            words[base + j] = wl[j];
        }
    }
    let off_tw = off_twiddle_w();
    words[off_tw..off_tw + twiddle_mont.len()].copy_from_slice(twiddle_mont);
    words.into_iter().flat_map(|w| w.to_le_bytes()).collect()
}

fn read_u32_at_byte(buf: &[u8], byte_off: usize) -> u32 {
    u32::from_le_bytes(buf[byte_off..byte_off + 4].try_into().unwrap())
}

fn unpack_coeffs(buf: &[u8], n: usize) -> Vec<Scalar> {
    let off_d = off_data_w();
    let base_byte = off_d * 4;
    let mut v = Vec::with_capacity(n);
    for i in 0..n {
        let mut limbs = [0u32; BLS12_381_FR_U32_LIMBS];
        for j in 0..BLS12_381_FR_U32_LIMBS {
            let bo = base_byte + (i * BLS12_381_FR_U32_LIMBS + j) * 4;
            limbs[j] = read_u32_at_byte(buf, bo);
        }
        v.push(scalar_from_montgomery_u32_limbs(&limbs));
    }
    v
}

fn spirv_words(bytes: &'static [u8]) -> Result<Vec<u32>> {
    read_spv(&mut Cursor::new(bytes)).context("read_spv fr_ntt_general")
}

/// Forward NTT: same result as CPU [`crate::ntt::fr_ntt_inplace`] with [`crate::ntt::fr_omega`].
///
/// §8.2 B.1: uses radix-4 DIT stages to halve the dispatch count.
/// Even `log_n`: `log_n/2` radix-4 stages.
/// Odd `log_n`: `(log_n-1)/2` radix-4 stages then 1 trailing radix-2 stage.
pub fn run_fr_ntt_general_forward_gpu(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
) -> Result<Vec<Scalar>> {
    run_fr_ntt_general_forward_gpu_impl(dev, coeffs, None)
}

/// §8.2 **B.5** — Forward Fr NTT like [`run_fr_ntt_general_forward_gpu`], but applies
/// `coeffs[i] *= distribute_base^i` while packing SSBO data (same semantics as GPU
/// [`crate::fr_pointwise_gpu::run_fr_coeff_wise_mult_gpu`] against powers of `distribute_base`).
/// Saves one full vector dispatch vs separate distribute + NTT.
pub fn run_fr_ntt_general_forward_gpu_distribute(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
    distribute_base: Scalar,
) -> Result<Vec<Scalar>> {
    run_fr_ntt_general_forward_gpu_impl(dev, coeffs, Some(distribute_base))
}

fn run_fr_ntt_general_forward_gpu_impl(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
    distribute_base: Option<Scalar>,
) -> Result<Vec<Scalar>> {
    let n = coeffs.len();
    if n == 0 {
        return Ok(vec![]);
    }
    anyhow::ensure!(n.is_power_of_two(), "n must be a power of two");
    anyhow::ensure!(
        n <= FR_NTT_GENERAL_MAX_N,
        "n {n} exceeds FR_NTT_GENERAL_MAX_N"
    );
    let log_n = n.trailing_zeros();
    let plan = FrNttPlan::try_new(log_n)?;

    let sched_wlens = radix4_schedule_wlens(n, plan.omega, log_n);
    let stage_limbs: Vec<[u32; BLS12_381_FR_U32_LIMBS]> = sched_wlens
        .iter()
        .map(|s| scalar_montgomery_u32_limbs(s))
        .collect();

    let (twiddle_blob, radix4_tw_off, radix2_tw) =
        twiddle_blob_radix4_stages(n, log_n, &sched_wlens);
    let packed = pack_buf(
        n,
        log_n,
        coeffs,
        &stage_limbs,
        None,
        &twiddle_blob,
        distribute_base,
    );
    let buf_sz = fr_ntt_general_buf_bytes() as u64;
    let read_len = (off_data_w() + n * BLS12_381_FR_U32_LIMBS) * 4;

    let w_bitrev = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_bitrev_scatter.spv"
    )))?;
    let w_copy = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_copy_scratch_to_data.spv"
    )))?;
    let w_radix2 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix2_stage_spec.spv"
    )))?;
    let w_radix4 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix4_fwd_stage.spv"
    )))?;

    let radix2_lx_le = spec_constant_u32_le(WG);
    let radix2_spec_map = [spec_map_u32(0, 0)];
    let radix2_stage_spec = ComputeShaderStageSpec {
        map_entries: &radix2_spec_map,
        data: &radix2_lx_le,
    };

    let dn = n as u32;
    let mut passes: Vec<ComputePassDesc<'_>> = Vec::new();
    passes.push(ComputePassDesc {
        spirv_words: &w_bitrev,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    passes.push(ComputePassDesc {
        spirv_words: &w_copy,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });

    let (k4, k2) = fr_ntt_radix4_stage_counts(log_n);
    // k4 radix-4 stages first (quarter = 4^i, twiddle table offset per stage).
    for i in 0..k4 {
        passes.push(ComputePassDesc {
            spirv_words: &w_radix4,
            dispatch: dispatch_for_quarter(dn),
            push_constants: push_radix_wide(4u32.pow(i), radix4_tw_off[i as usize]),
            stage_spec: None,
        });
    }
    // Optional trailing radix-2 stage (odd log_n): DIT stage log_n-1, half = n/2.
    if k2 == 1 {
        let (tw_off, stage) = radix2_tw.expect("k2==1 implies radix2 twiddle");
        passes.push(ComputePassDesc {
            spirv_words: &w_radix2,
            dispatch: dispatch_for_half(dn),
            push_constants: push_radix2_twiddle(stage, tw_off),
            stage_spec: Some(radix2_stage_spec),
        });
    }

    let mut readback = vec![0u8; read_len];
    unsafe {
        run_compute_passes_1x_storage_buffer(
            dev,
            buf_sz,
            buf_sz,
            &packed,
            &passes,
            read_len,
            &mut readback,
        )?;
    }
    Ok(unpack_coeffs(&readback, n))
}

/// Inverse NTT: same result as CPU [`crate::ntt::fr_intt_inplace`] with [`crate::ntt::fr_omega`].
///
/// §8.2 B.1: uses inverse radix-4 DIT stages (same as forward but with `omega_inv` twiddles
/// and the conjugate 4th-root `i_unit_inv = omega^(-n/4)`), then scales by n⁻¹.
pub fn run_fr_ntt_general_inverse_gpu(dev: &VulkanDevice, evals: &[Scalar]) -> Result<Vec<Scalar>> {
    let n = evals.len();
    if n == 0 {
        return Ok(vec![]);
    }
    anyhow::ensure!(n.is_power_of_two(), "n must be a power of two");
    anyhow::ensure!(
        n <= FR_NTT_GENERAL_MAX_N,
        "n {n} exceeds FR_NTT_GENERAL_MAX_N"
    );
    let log_n = n.trailing_zeros();
    let plan = FrNttPlan::try_new(log_n)?;

    let sched_wlens = radix4_schedule_wlens(n, plan.omega_inv, log_n);
    let stage_limbs: Vec<[u32; BLS12_381_FR_U32_LIMBS]> = sched_wlens
        .iter()
        .map(|s| scalar_montgomery_u32_limbs(s))
        .collect();
    let n_inv = Scalar::from(n as u64).invert().unwrap();
    let n_inv_limbs = scalar_montgomery_u32_limbs(&n_inv);

    let (twiddle_blob, radix4_tw_off, radix2_tw) =
        twiddle_blob_radix4_stages(n, log_n, &sched_wlens);
    let packed = pack_buf(
        n,
        log_n,
        evals,
        &stage_limbs,
        Some(&n_inv_limbs),
        &twiddle_blob,
        None,
    );
    let buf_sz = fr_ntt_general_buf_bytes() as u64;
    let read_len = (off_data_w() + n * BLS12_381_FR_U32_LIMBS) * 4;

    let w_bitrev = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_bitrev_scatter.spv"
    )))?;
    let w_copy = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_copy_scratch_to_data.spv"
    )))?;
    let w_radix2 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix2_stage_spec.spv"
    )))?;
    let w_radix4 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix4_inv_stage.spv"
    )))?;
    let w_scale = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_scale_ninv.spv"
    )))?;

    let radix2_lx_le_inv = spec_constant_u32_le(WG);
    let radix2_spec_map_inv = [spec_map_u32(0, 0)];
    let radix2_stage_spec_inv = ComputeShaderStageSpec {
        map_entries: &radix2_spec_map_inv,
        data: &radix2_lx_le_inv,
    };

    let dn = n as u32;
    let mut passes: Vec<ComputePassDesc<'_>> = Vec::new();
    passes.push(ComputePassDesc {
        spirv_words: &w_bitrev,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    passes.push(ComputePassDesc {
        spirv_words: &w_copy,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });

    let (k4, k2) = fr_ntt_radix4_stage_counts(log_n);
    for i in 0..k4 {
        passes.push(ComputePassDesc {
            spirv_words: &w_radix4,
            dispatch: dispatch_for_quarter(dn),
            push_constants: push_radix_wide(4u32.pow(i), radix4_tw_off[i as usize]),
            stage_spec: None,
        });
    }
    if k2 == 1 {
        let (tw_off, stage) = radix2_tw.expect("k2==1 implies radix2 twiddle");
        passes.push(ComputePassDesc {
            spirv_words: &w_radix2,
            dispatch: dispatch_for_half(dn),
            push_constants: push_radix2_twiddle(stage, tw_off),
            stage_spec: Some(radix2_stage_spec_inv),
        });
    }
    passes.push(ComputePassDesc {
        spirv_words: &w_scale,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });

    let mut readback = vec![0u8; read_len];
    unsafe {
        run_compute_passes_1x_storage_buffer(
            dev,
            buf_sz,
            buf_sz,
            &packed,
            &passes,
            read_len,
            &mut readback,
        )?;
    }
    Ok(unpack_coeffs(&readback, n))
}

/// Forward then inverse on the GPU (round-trip sanity) — radix-4 schedule.
pub fn run_fr_ntt_general_roundtrip_gpu(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
) -> Result<Vec<Scalar>> {
    let fwd = run_fr_ntt_general_forward_gpu(dev, coeffs)?;
    run_fr_ntt_general_inverse_gpu(dev, &fwd)
}

/// §8.2 B.2 — Forward NTT using the radix-8 schedule.
///
/// Dispatches `log_n/3` radix-8 stages (+ 1 trailing radix-4 or radix-2 when `log_n % 3 ≠ 0`),
/// reducing dispatch count to `ceil(log_n/3) + 2` vs `log_n/2 + 2` for radix-4.
pub fn run_fr_ntt_radix8_forward_gpu(dev: &VulkanDevice, coeffs: &[Scalar]) -> Result<Vec<Scalar>> {
    run_fr_ntt_radix8_forward_gpu_impl(dev, coeffs, None)
}

/// Forward radix-8 NTT with §8.2 **B.5** pack fusion (`coeffs[i] *= base^i`), same as
/// [`run_fr_ntt_general_forward_gpu_distribute`] but using the radix-8 butterfly schedule (`n ≥ 8`).
pub fn run_fr_ntt_radix8_forward_gpu_distribute(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
    distribute_base: Scalar,
) -> Result<Vec<Scalar>> {
    run_fr_ntt_radix8_forward_gpu_impl(dev, coeffs, Some(distribute_base))
}

fn run_fr_ntt_radix8_forward_gpu_impl(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
    distribute_base: Option<Scalar>,
) -> Result<Vec<Scalar>> {
    let n = coeffs.len();
    if n == 0 {
        return Ok(vec![]);
    }
    anyhow::ensure!(n.is_power_of_two(), "n must be a power of two");
    anyhow::ensure!(
        n <= FR_NTT_GENERAL_MAX_N,
        "n {n} exceeds FR_NTT_GENERAL_MAX_N"
    );
    anyhow::ensure!(n >= 8, "radix-8 requires n >= 8");
    let log_n = n.trailing_zeros();
    let plan = FrNttPlan::try_new(log_n)?;

    let sched_wlens = radix8_schedule_wlens(n, plan.omega, log_n);
    let stage_limbs: Vec<[u32; BLS12_381_FR_U32_LIMBS]> = sched_wlens
        .iter()
        .map(|s| scalar_montgomery_u32_limbs(s))
        .collect();

    let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
    let (twiddle_blob, radix8_tw_off, radix4_tail_tw_off, radix2_tail_tw_off) =
        twiddle_blob_radix8_stages(n, log_n, &sched_wlens, k8, rem);
    let packed = pack_buf(
        n,
        log_n,
        coeffs,
        &stage_limbs,
        None,
        &twiddle_blob,
        distribute_base,
    );
    let buf_sz = fr_ntt_general_buf_bytes() as u64;
    let read_len = (off_data_w() + n * BLS12_381_FR_U32_LIMBS) * 4;

    let w_bitrev = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_bitrev_scatter.spv"
    )))?;
    let w_copy = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_copy_scratch_to_data.spv"
    )))?;
    let w_radix2 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix2_stage_spec.spv"
    )))?;
    let w_radix4 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix4_fwd_stage.spv"
    )))?;
    let w_radix8 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix8_fwd_stage.spv"
    )))?;

    let radix2_lx_le_r8fwd = spec_constant_u32_le(WG);
    let radix2_spec_map_r8fwd = [spec_map_u32(0, 0)];
    let radix2_stage_spec_r8fwd = ComputeShaderStageSpec {
        map_entries: &radix2_spec_map_r8fwd,
        data: &radix2_lx_le_r8fwd,
    };

    let dn = n as u32;
    let mut passes: Vec<ComputePassDesc<'_>> = Vec::new();
    passes.push(ComputePassDesc {
        spirv_words: &w_bitrev,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    passes.push(ComputePassDesc {
        spirv_words: &w_copy,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });

    let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
    for i in 0..k8 {
        passes.push(ComputePassDesc {
            spirv_words: &w_radix8,
            dispatch: dispatch_for_octet(dn),
            push_constants: push_radix_wide(8u32.pow(i), radix8_tw_off[i as usize]),
            stage_spec: None,
        });
    }
    match rem {
        1 => passes.push(ComputePassDesc {
            spirv_words: &w_radix2,
            dispatch: dispatch_for_half(dn),
            push_constants: push_radix2_twiddle(
                log_n - 1,
                radix2_tail_tw_off.expect("rem==1 implies radix-2 tail twiddle"),
            ),
            stage_spec: Some(radix2_stage_spec_r8fwd),
        }),
        2 => passes.push(ComputePassDesc {
            spirv_words: &w_radix4,
            dispatch: dispatch_for_quarter(dn),
            push_constants: push_radix_wide(
                8u32.pow(k8),
                radix4_tail_tw_off.expect("rem==2 implies radix-4 tail twiddle"),
            ),
            stage_spec: None,
        }),
        _ => {}
    }

    let mut readback = vec![0u8; read_len];
    unsafe {
        run_compute_passes_1x_storage_buffer(
            dev,
            buf_sz,
            buf_sz,
            &packed,
            &passes,
            read_len,
            &mut readback,
        )?;
    }
    Ok(unpack_coeffs(&readback, n))
}

/// §8.2 B.2 — Inverse NTT using the radix-8 schedule.
pub fn run_fr_ntt_radix8_inverse_gpu(dev: &VulkanDevice, evals: &[Scalar]) -> Result<Vec<Scalar>> {
    let n = evals.len();
    if n == 0 {
        return Ok(vec![]);
    }
    anyhow::ensure!(n.is_power_of_two(), "n must be a power of two");
    anyhow::ensure!(
        n <= FR_NTT_GENERAL_MAX_N,
        "n {n} exceeds FR_NTT_GENERAL_MAX_N"
    );
    anyhow::ensure!(n >= 8, "radix-8 requires n >= 8");
    let log_n = n.trailing_zeros();
    let plan = FrNttPlan::try_new(log_n)?;

    let sched_wlens = radix8_schedule_wlens(n, plan.omega_inv, log_n);
    let stage_limbs: Vec<[u32; BLS12_381_FR_U32_LIMBS]> = sched_wlens
        .iter()
        .map(|s| scalar_montgomery_u32_limbs(s))
        .collect();
    let n_inv = Scalar::from(n as u64).invert().unwrap();
    let n_inv_limbs = scalar_montgomery_u32_limbs(&n_inv);

    let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
    let (twiddle_blob, radix8_tw_off, radix4_tail_tw_off, radix2_tail_tw_off) =
        twiddle_blob_radix8_stages(n, log_n, &sched_wlens, k8, rem);
    let packed = pack_buf(
        n,
        log_n,
        evals,
        &stage_limbs,
        Some(&n_inv_limbs),
        &twiddle_blob,
        None,
    );
    let buf_sz = fr_ntt_general_buf_bytes() as u64;
    let read_len = (off_data_w() + n * BLS12_381_FR_U32_LIMBS) * 4;

    let w_bitrev = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_bitrev_scatter.spv"
    )))?;
    let w_copy = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_copy_scratch_to_data.spv"
    )))?;
    let w_radix2 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix2_stage_spec.spv"
    )))?;
    let w_radix4 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix4_inv_stage.spv"
    )))?;
    let w_radix8 = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_radix8_inv_stage.spv"
    )))?;
    let w_scale = spirv_words(include_bytes!(concat!(
        env!("OUT_DIR"),
        "/fr_ntt_general_scale_ninv.spv"
    )))?;

    let radix2_lx_le_r8inv = spec_constant_u32_le(WG);
    let radix2_spec_map_r8inv = [spec_map_u32(0, 0)];
    let radix2_stage_spec_r8inv = ComputeShaderStageSpec {
        map_entries: &radix2_spec_map_r8inv,
        data: &radix2_lx_le_r8inv,
    };

    let dn = n as u32;
    let mut passes: Vec<ComputePassDesc<'_>> = Vec::new();
    passes.push(ComputePassDesc {
        spirv_words: &w_bitrev,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });
    passes.push(ComputePassDesc {
        spirv_words: &w_copy,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });

    for i in 0..k8 {
        passes.push(ComputePassDesc {
            spirv_words: &w_radix8,
            dispatch: dispatch_for_octet(dn),
            push_constants: push_radix_wide(8u32.pow(i), radix8_tw_off[i as usize]),
            stage_spec: None,
        });
    }
    match rem {
        1 => passes.push(ComputePassDesc {
            spirv_words: &w_radix2,
            dispatch: dispatch_for_half(dn),
            push_constants: push_radix2_twiddle(
                log_n - 1,
                radix2_tail_tw_off.expect("rem==1 implies radix-2 tail twiddle"),
            ),
            stage_spec: Some(radix2_stage_spec_r8inv),
        }),
        2 => passes.push(ComputePassDesc {
            spirv_words: &w_radix4,
            dispatch: dispatch_for_quarter(dn),
            push_constants: push_radix_wide(
                8u32.pow(k8),
                radix4_tail_tw_off.expect("rem==2 implies radix-4 tail twiddle"),
            ),
            stage_spec: None,
        }),
        _ => {}
    }
    passes.push(ComputePassDesc {
        spirv_words: &w_scale,
        dispatch: dispatch_for_n(dn),
        push_constants: push_zero(),
        stage_spec: None,
    });

    let mut readback = vec![0u8; read_len];
    unsafe {
        run_compute_passes_1x_storage_buffer(
            dev,
            buf_sz,
            buf_sz,
            &packed,
            &passes,
            read_len,
            &mut readback,
        )?;
    }
    Ok(unpack_coeffs(&readback, n))
}

/// Radix-8 round-trip: forward then inverse.
pub fn run_fr_ntt_radix8_roundtrip_gpu(
    dev: &VulkanDevice,
    coeffs: &[Scalar],
) -> Result<Vec<Scalar>> {
    let fwd = run_fr_ntt_radix8_forward_gpu(dev, coeffs)?;
    run_fr_ntt_radix8_inverse_gpu(dev, &fwd)
}

#[cfg(test)]
mod twiddle_blob_tests {
    use super::*;
    use crate::ntt::FrNttPlan;

    #[test]
    fn twiddle_tables_stay_within_ssbo_cap() {
        for log_n in 1u32..=14 {
            let n = 1usize << log_n;
            let plan = FrNttPlan::try_new(log_n).unwrap();
            let s4 = radix4_schedule_wlens(n, plan.omega, log_n);
            let (b4, _, _) = twiddle_blob_radix4_stages(n, log_n, &s4);
            assert!(
                b4.len() <= FR_NTT_GENERAL_TWIDDLE_U32_LEN,
                "radix4 log_n={log_n} len {}",
                b4.len()
            );

            if log_n >= 3 {
                let s8 = radix8_schedule_wlens(n, plan.omega, log_n);
                let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
                let (b8, _, _, _) = twiddle_blob_radix8_stages(n, log_n, &s8, k8, rem);
                assert!(
                    b8.len() <= FR_NTT_GENERAL_TWIDDLE_U32_LEN,
                    "radix8 log_n={log_n} len {}",
                    b8.len()
                );
            }
        }
    }
}

#[cfg(test)]
mod radix2_pass_count_tests {
    use super::*;

    #[test]
    fn forward_pass_count_matches_gpu_dispatch_list() {
        for log_n in 1u32..=14 {
            assert_eq!(fr_ntt_general_forward_pass_count_radix2(log_n), 2 + log_n);
        }
    }

    #[test]
    fn inverse_pass_count_matches_gpu_dispatch_list() {
        for log_n in 1u32..=14 {
            assert_eq!(fr_ntt_general_inverse_pass_count_radix2(log_n), 3 + log_n);
        }
    }
}
