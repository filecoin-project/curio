//! Correctness tests for the §8.2 B.2 radix-8 DIT NTT GPU implementation.
//!
//! Each GPU test verifies that `run_fr_ntt_radix8_forward_gpu` /
//! `run_fr_ntt_radix8_inverse_gpu` match the CPU reference for various `n`.
//! Stage-count tests are CPU-only and always run.

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use ff::Field;
use cuzk_vk::fr_ntt_general_gpu::{
    fr_ntt_general_forward_pass_count_radix4, fr_ntt_general_forward_pass_count_radix8,
    fr_ntt_general_inverse_pass_count_radix8, fr_ntt_radix8_stage_counts,
    run_fr_ntt_radix8_forward_gpu, run_fr_ntt_radix8_inverse_gpu,
    run_fr_ntt_radix8_roundtrip_gpu,
};
use cuzk_vk::ntt::{fr_intt_inplace, fr_ntt_inplace, fr_omega};

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
fn radix8_stage_counts_exact() {
    // log_n % 3 == 0: k8 stages, no trailing.
    for log_n in [3u32, 6, 9, 12] {
        let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
        assert_eq!(k8, log_n / 3, "log_n={log_n}");
        assert_eq!(rem, 0, "log_n={log_n}: expected no trailing");
    }
    // log_n % 3 == 1: 1 trailing radix-2.
    for log_n in [4u32, 7, 10, 13] {
        let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
        assert_eq!(k8, log_n / 3, "log_n={log_n}");
        assert_eq!(rem, 1, "log_n={log_n}: expected trailing r2");
    }
    // log_n % 3 == 2: 1 trailing radix-4.
    for log_n in [5u32, 8, 11, 14] {
        let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
        assert_eq!(k8, log_n / 3, "log_n={log_n}");
        assert_eq!(rem, 2, "log_n={log_n}: expected trailing r4");
    }
}

#[test]
fn radix8_pass_counts() {
    for log_n in 3u32..=14 {
        let fwd = fr_ntt_general_forward_pass_count_radix8(log_n);
        let inv = fr_ntt_general_inverse_pass_count_radix8(log_n);
        let (k8, rem) = fr_ntt_radix8_stage_counts(log_n);
        assert_eq!(fwd, 2 + k8 + rem.min(1), "fwd log_n={log_n}");
        assert_eq!(inv, fwd + 1, "inv log_n={log_n}");
    }
}

#[test]
fn radix8_fewer_dispatches_than_radix4() {
    for log_n in 3u32..=14 {
        let r4 = fr_ntt_general_forward_pass_count_radix4(log_n);
        let r8 = fr_ntt_general_forward_pass_count_radix8(log_n);
        assert!(
            r8 <= r4,
            "log_n={log_n}: radix-8 ({r8}) should use ≤ dispatches than radix-4 ({r4})"
        );
    }
}

// ── GPU correctness tests ─────────────────────────────────────────────────────

fn skip_without_vulkan() -> bool {
    matches!(
        std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(),
        Ok("1") | Err(_)
    )
}

/// Forward NTT for log_n divisible by 3 (pure radix-8 path).
#[test]
fn fr_ntt_radix8_forward_div3_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [3u32, 6, 9, 12] {
        let n = 1 << log_n;
        let coeffs = make_scalars(n, log_n as u64);
        let expected = cpu_ntt_forward(&coeffs);
        let got = run_fr_ntt_radix8_forward_gpu(&dev, &coeffs)
            .unwrap_or_else(|e| panic!("fwd n={n}: {e}"));
        assert_eq!(got, expected, "forward mismatch at log_n={log_n}");
    }
}

/// Forward NTT for log_n ≡ 1 (mod 3) — radix-8 + trailing radix-2.
#[test]
fn fr_ntt_radix8_forward_rem1_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [4u32, 7, 10, 13] {
        let n = 1 << log_n;
        let coeffs = make_scalars(n, log_n as u64 + 100);
        let expected = cpu_ntt_forward(&coeffs);
        let got = run_fr_ntt_radix8_forward_gpu(&dev, &coeffs)
            .unwrap_or_else(|e| panic!("fwd n={n}: {e}"));
        assert_eq!(got, expected, "forward mismatch at log_n={log_n}");
    }
}

/// Forward NTT for log_n ≡ 2 (mod 3) — radix-8 + trailing radix-4.
#[test]
fn fr_ntt_radix8_forward_rem2_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [5u32, 8, 11, 14] {
        let n = 1 << log_n;
        let coeffs = make_scalars(n, log_n as u64 + 200);
        let expected = cpu_ntt_forward(&coeffs);
        let got = run_fr_ntt_radix8_forward_gpu(&dev, &coeffs)
            .unwrap_or_else(|e| panic!("fwd n={n}: {e}"));
        assert_eq!(got, expected, "forward mismatch at log_n={log_n}");
    }
}

/// Inverse NTT for log_n divisible by 3 (pure radix-8 path).
#[test]
fn fr_ntt_radix8_inverse_div3_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [3u32, 6, 9, 12] {
        let n = 1 << log_n;
        let evals = make_scalars(n, log_n as u64 + 300);
        let expected = cpu_ntt_inverse(&evals);
        let got = run_fr_ntt_radix8_inverse_gpu(&dev, &evals)
            .unwrap_or_else(|e| panic!("inv n={n}: {e}"));
        assert_eq!(got, expected, "inverse mismatch at log_n={log_n}");
    }
}

/// Inverse NTT for log_n ≡ 1 (mod 3) — radix-8 + trailing radix-2.
#[test]
fn fr_ntt_radix8_inverse_rem1_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [4u32, 7, 10, 13] {
        let n = 1 << log_n;
        let evals = make_scalars(n, log_n as u64 + 400);
        let expected = cpu_ntt_inverse(&evals);
        let got = run_fr_ntt_radix8_inverse_gpu(&dev, &evals)
            .unwrap_or_else(|e| panic!("inv n={n}: {e}"));
        assert_eq!(got, expected, "inverse mismatch at log_n={log_n}");
    }
}

/// Inverse NTT for log_n ≡ 2 (mod 3) — radix-8 + trailing radix-4.
#[test]
fn fr_ntt_radix8_inverse_rem2_matches_cpu() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [5u32, 8, 11, 14] {
        let n = 1 << log_n;
        let evals = make_scalars(n, log_n as u64 + 500);
        let expected = cpu_ntt_inverse(&evals);
        let got = run_fr_ntt_radix8_inverse_gpu(&dev, &evals)
            .unwrap_or_else(|e| panic!("inv n={n}: {e}"));
        assert_eq!(got, expected, "inverse mismatch at log_n={log_n}");
    }
}

/// Round-trip (forward then inverse = identity) for all three schedule variants.
#[test]
fn fr_ntt_radix8_roundtrip_matches_input() {
    if skip_without_vulkan() {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE");
        return;
    }
    let dev = VulkanDevice::new().expect("VulkanDevice::new");
    for log_n in [3u32, 4, 5, 6, 7, 8, 9, 12, 14] {
        let n = 1 << log_n;
        let coeffs = make_scalars(n, log_n as u64 + 600);
        let got = run_fr_ntt_radix8_roundtrip_gpu(&dev, &coeffs)
            .unwrap_or_else(|e| panic!("roundtrip n={n}: {e}"));
        assert_eq!(got, coeffs, "roundtrip failed at log_n={log_n}");
    }
}
