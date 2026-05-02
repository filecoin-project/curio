//! Correctness tests for the §8.2 B.1 radix-4 DIT NTT GPU implementation.
//!
//! Each test verifies that `run_fr_ntt_general_forward_gpu` / `run_fr_ntt_general_inverse_gpu`
//! (now using radix-4 stages internally) match the CPU reference for various `n`.

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fr_ntt_general_gpu::{
    fr_ntt_general_forward_pass_count_radix4, fr_ntt_general_inverse_pass_count_radix4,
    fr_ntt_radix4_stage_counts, run_fr_ntt_general_forward_gpu,
    run_fr_ntt_general_inverse_gpu,
};
use cuzk_vk::ntt::{fr_intt_inplace, fr_ntt_inplace, fr_omega};
use ff::Field;

fn make_scalars(n: usize, seed: u64) -> Vec<Scalar> {
    let mut v = Vec::with_capacity(n);
    let mut x = Scalar::from(seed + 1);
    for _ in 0..n {
        v.push(x);
        x = x.square();
        x += Scalar::ONE;
    }
    v
}

fn cpu_ntt_forward(coeffs: &[Scalar]) -> Vec<Scalar> {
    let n = coeffs.len();
    let omega = fr_omega(n);
    let mut v = coeffs.to_vec();
    fr_ntt_inplace(&mut v, omega);
    v
}

fn cpu_ntt_inverse(evals: &[Scalar]) -> Vec<Scalar> {
    let n = evals.len();
    let omega = fr_omega(n);
    let mut v = evals.to_vec();
    fr_intt_inplace(&mut v, omega);
    v
}

// ── stage-count unit tests (no GPU required) ─────────────────────────────────

#[test]
fn radix4_stage_counts_even_log_n() {
    // Even log_n: log_n/2 radix-4 stages, no trailing radix-2.
    for log_n in [2u32, 4, 6, 8, 10, 12, 14] {
        let (k4, k2) = fr_ntt_radix4_stage_counts(log_n);
        assert_eq!(k2, 0, "log_n={log_n}: expected 0 trailing radix-2");
        assert_eq!(k4, log_n / 2, "log_n={log_n}");
    }
}

#[test]
fn radix4_stage_counts_odd_log_n() {
    // Odd log_n: (log_n-1)/2 radix-4 stages + 1 trailing radix-2.
    for log_n in [1u32, 3, 5, 7, 9, 11, 13] {
        let (k4, k2) = fr_ntt_radix4_stage_counts(log_n);
        assert_eq!(k2, 1, "log_n={log_n}: expected 1 trailing radix-2");
        assert_eq!(k4, log_n / 2, "log_n={log_n}");
    }
}

#[test]
fn radix4_pass_counts() {
    for log_n in 1u32..=14 {
        let fwd = fr_ntt_general_forward_pass_count_radix4(log_n);
        let inv = fr_ntt_general_inverse_pass_count_radix4(log_n);
        let expected_fwd = 2 + log_n.div_ceil(2);
        assert_eq!(fwd, expected_fwd, "fwd log_n={log_n}");
        assert_eq!(inv, fwd + 1, "inv log_n={log_n}");
    }
}

// ── GPU correctness tests ─────────────────────────────────────────────────────

fn skip_without_vulkan() -> bool {
    matches!(
        std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(),
        Ok("1") | Err(_)
    )
}

/// Forward NTT for even log_n (pure radix-4 path).
#[test]
fn fr_ntt_radix4_forward_even_log_n_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [2u32, 4, 6, 8, 10] {
        let n = 1 << log_n;
        let coeffs = make_scalars(n, log_n as u64);
        let expected = cpu_ntt_forward(&coeffs);
        let got = run_fr_ntt_general_forward_gpu(&dev, &coeffs)
            .unwrap_or_else(|e| panic!("fwd n={n}: {e}"));
        assert_eq!(got, expected, "forward mismatch at log_n={log_n}");
    }
}

/// Forward NTT for odd log_n (1 radix-2 + radix-4 path).
#[test]
fn fr_ntt_radix4_forward_odd_log_n_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [1u32, 3, 5, 7, 9] {
        let n = 1 << log_n;
        let coeffs = make_scalars(n, log_n as u64 + 100);
        let expected = cpu_ntt_forward(&coeffs);
        let got = run_fr_ntt_general_forward_gpu(&dev, &coeffs)
            .unwrap_or_else(|e| panic!("fwd n={n}: {e}"));
        assert_eq!(got, expected, "forward mismatch at log_n={log_n}");
    }
}

/// Inverse NTT for even log_n (pure radix-4 path).
#[test]
fn fr_ntt_radix4_inverse_even_log_n_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [2u32, 4, 6, 8, 10] {
        let n = 1 << log_n;
        let evals = make_scalars(n, log_n as u64 + 200);
        let expected = cpu_ntt_inverse(&evals);
        let got = run_fr_ntt_general_inverse_gpu(&dev, &evals)
            .unwrap_or_else(|e| panic!("inv n={n}: {e}"));
        assert_eq!(got, expected, "inverse mismatch at log_n={log_n}");
    }
}

/// Inverse NTT for odd log_n (1 radix-2 + radix-4 path).
#[test]
fn fr_ntt_radix4_inverse_odd_log_n_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [1u32, 3, 5, 7, 9] {
        let n = 1 << log_n;
        let evals = make_scalars(n, log_n as u64 + 300);
        let expected = cpu_ntt_inverse(&evals);
        let got = run_fr_ntt_general_inverse_gpu(&dev, &evals)
            .unwrap_or_else(|e| panic!("inv n={n}: {e}"));
        assert_eq!(got, expected, "inverse mismatch at log_n={log_n}");
    }
}

/// Round-trip (fwd then inv = identity) for multiple sizes.
#[test]
fn fr_ntt_radix4_roundtrip_matches_input() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [1u32, 2, 3, 4, 6, 8, 10, 12, 14] {
        let n = 1 << log_n;
        let coeffs = make_scalars(n, log_n as u64 + 400);
        let fwd = run_fr_ntt_general_forward_gpu(&dev, &coeffs)
            .unwrap_or_else(|e| panic!("fwd n={n}: {e}"));
        let got = run_fr_ntt_general_inverse_gpu(&dev, &fwd)
            .unwrap_or_else(|e| panic!("inv n={n}: {e}"));
        assert_eq!(got, coeffs, "roundtrip failed at log_n={log_n}");
    }
}

/// Radix-4 dispatch count is less than or equal to radix-2 for all sizes.
#[test]
fn radix4_fewer_dispatches_than_radix2() {
    use cuzk_vk::fr_ntt_general_gpu::{
        fr_ntt_general_forward_pass_count_radix2, fr_ntt_general_forward_pass_count_radix4,
    };
    for log_n in 2u32..=14 {
        let r2 = fr_ntt_general_forward_pass_count_radix2(log_n);
        let r4 = fr_ntt_general_forward_pass_count_radix4(log_n);
        assert!(r4 <= r2, "log_n={log_n}: radix-4 ({r4}) should use ≤ dispatches than radix-2 ({r2})");
    }
}
