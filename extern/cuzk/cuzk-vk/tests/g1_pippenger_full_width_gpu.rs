//! §8.1 correctness test: full 255-bit scalar Pippenger MSM on the GPU.
//!
//! Tests [`run_g1_pippenger_msm_gpu`] against the CPU Pippenger reference
//! [`g1_pippenger_msm_cpu`] and against `G1Projective::multi_exp` for various n.
//!
//! GPU cases are skipped when `CUZK_VK_SKIP_SMOKE` is unset or `1` (same as other
//! `cuzk-vk` integration tests), or when `VulkanDevice::new` fails (no loader/ICD).
//! Large cases (n≥512) can also be skipped with `CUZK_VK_SKIP_LARGE_GPU_TESTS=1`.

use std::sync::Arc;

use blstrs::{G1Affine, G1Projective, Scalar};
use ff::Field;
use group::{Curve, Group};
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

use cuzk_vk::VulkanDevice;
use cuzk_vk::g1_pippenger_bucket_gpu::{
    g1_pippenger_msm_cpu, g1_pippenger_msm_cpu_batch, g1_pippenger_msm_cpu_batch_shared_bases,
    pippenger_plan_batch_tiles, run_g1_pippenger_msm_gpu, run_g1_pippenger_msm_gpu_batch,
    run_g1_pippenger_msm_gpu_batch_shared_bases, G1PippengerBatchItem,
};

/// Skip when `CUZK_VK_SKIP_SMOKE` is unset or `1`, or when no Vulkan loader/ICD.
fn dev_or_skip() -> Option<Arc<VulkanDevice>> {
    if matches!(
        std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(),
        Ok("1") | Err(_)
    ) {
        eprintln!("skip: CUZK_VK_SKIP_SMOKE (set to 0 to run GPU Pippenger tests)");
        return None;
    }
    match VulkanDevice::new() {
        Ok(d) => Some(Arc::new(d)),
        Err(e) => {
            eprintln!("skip: VulkanDevice::new failed ({e})");
            None
        }
    }
}

fn random_bases_and_scalars(n: usize, seed: u64) -> (Vec<G1Affine>, Vec<Scalar>) {
    let mut rng = ChaCha8Rng::seed_from_u64(seed);
    let bases: Vec<G1Affine> = (0..n)
        .map(|_| (G1Projective::random(&mut rng)).to_affine())
        .collect();
    let scalars: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    (bases, scalars)
}

fn multi_exp_ref(bases: &[G1Affine], scalars: &[Scalar]) -> G1Projective {
    let pts: Vec<G1Projective> = bases.iter().map(|&a| G1Projective::from(a)).collect();
    G1Projective::multi_exp(&pts, scalars)
}

/// Tiny n=8 sanity check with w=4 (16 buckets, 2 windows).
#[test]
fn g1_pippenger_w4_n8_matches_multi_exp() {
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let (bases, scalars) = random_bases_and_scalars(8, 0xabc);
    let window_bits = 4u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 8, window_bits)
        .expect("run_g1_pippenger_msm_gpu");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=4 n=8 GPU vs multi_exp"
    );
}

/// CPU reference matches `multi_exp` (validates the windowing logic itself).
#[test]
fn g1_pippenger_cpu_matches_multi_exp_n64() {
    let (bases, scalars) = random_bases_and_scalars(64, 0xf00d);
    let window_bits = 8u32;
    let got = g1_pippenger_msm_cpu(&bases, &scalars, 64, window_bits);
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "CPU Pippenger w=8 n=64 vs multi_exp"
    );
}

/// n=64, window_bits=8 (32 windows of 8 bits → 256 buckets each).
#[test]
fn g1_pippenger_w8_n64_matches_multi_exp() {
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let (bases, scalars) = random_bases_and_scalars(64, 0xdeadbeef);
    let window_bits = 8u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 64, window_bits)
        .expect("run_g1_pippenger_msm_gpu w=8");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=8 n=64 GPU vs multi_exp"
    );
}

/// GPU matches CPU Pippenger reference at n=64 (validates GPU matches the reference algorithm).
#[test]
fn g1_pippenger_gpu_matches_cpu_n64() {
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let (bases, scalars) = random_bases_and_scalars(64, 0x1234);
    let window_bits = 12u32; // Apple M2 recommended window size.
    let gpu = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 64, window_bits)
        .expect("GPU w=12 n=64");
    let cpu = g1_pippenger_msm_cpu(&bases, &scalars, 64, window_bits);
    assert_eq!(gpu.to_affine(), cpu.to_affine(), "GPU vs CPU Pippenger w=12 n=64");
}

/// Larger n=512 test; skippable via `CUZK_VK_SKIP_LARGE_GPU_TESTS=1`.
#[test]
fn g1_pippenger_w12_n512_matches_multi_exp() {
    if std::env::var("CUZK_VK_SKIP_LARGE_GPU_TESTS").as_deref() == Ok("1") {
        return;
    }
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let (bases, scalars) = random_bases_and_scalars(512, 0x5555);
    let window_bits = 12u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 512, window_bits)
        .expect("GPU w=12 n=512");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=12 n=512 GPU vs multi_exp"
    );
}

/// n=1024 (2^10). Skippable via `CUZK_VK_SKIP_LARGE_GPU_TESTS=1`.
#[test]
fn g1_pippenger_w12_n1024_matches_multi_exp() {
    if std::env::var("CUZK_VK_SKIP_LARGE_GPU_TESTS").as_deref() == Ok("1") {
        return;
    }
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let (bases, scalars) = random_bases_and_scalars(1024, 0xbabe);
    let window_bits = 12u32;
    let got = run_g1_pippenger_msm_gpu(&dev, &bases, &scalars, 1024, window_bits)
        .expect("GPU w=12 n=1024");
    let want = multi_exp_ref(&bases, &scalars);
    assert_eq!(
        got.to_affine(),
        want.to_affine(),
        "w=12 n=1024 GPU vs multi_exp"
    );
}

/// §8.4 batched SSBO: two circuits per dispatch vs serial GPU MSM.
#[test]
fn g1_pippenger_batch_w8_n16_matches_serial_gpu() {
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let n = 16usize;
    let wb = 8u32;
    let (b0, s0) = random_bases_and_scalars(n, 0xba7c_0001);
    let (b1, s1) = random_bases_and_scalars(n, 0xba7d_0002);
    let circuits = [
        G1PippengerBatchItem {
            bases: &b0,
            scalars: &s0,
        },
        G1PippengerBatchItem {
            bases: &b1,
            scalars: &s1,
        },
    ];
    let batched = run_g1_pippenger_msm_gpu_batch(&dev, &circuits, wb).expect("batch msm");
    assert_eq!(batched.len(), 2);
    let g0 = run_g1_pippenger_msm_gpu(&dev, &b0, &s0, n, wb).expect("msm c0");
    let g1 = run_g1_pippenger_msm_gpu(&dev, &b1, &s1, n, wb).expect("msm c1");
    assert_eq!(batched[0].to_affine(), g0.to_affine(), "circuit 0");
    assert_eq!(batched[1].to_affine(), g1.to_affine(), "circuit 1");

    let cpu_b = g1_pippenger_msm_cpu_batch(&circuits, wb);
    assert_eq!(cpu_b[0].to_affine(), g0.to_affine());
    assert_eq!(cpu_b[1].to_affine(), g1.to_affine());
}

/// Variable `n` per circuit in one SSBO when sizes fit (`batch ≤ 128`, `batch*n_tot` limits).
#[test]
fn g1_pippenger_batch_unequal_n_matches_serial_gpu() {
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let wb = 8u32;
    let (b0, s0) = random_bases_and_scalars(8, 0xcafe_0001);
    let (b1, s1) = random_bases_and_scalars(24, 0xcafe_0002);
    let circuits = [
        G1PippengerBatchItem {
            bases: &b0,
            scalars: &s0,
        },
        G1PippengerBatchItem {
            bases: &b1,
            scalars: &s1,
        },
    ];
    let batched = run_g1_pippenger_msm_gpu_batch(&dev, &circuits, wb).expect("batch msm");
    let g0 = run_g1_pippenger_msm_gpu(&dev, &b0, &s0, 8, wb).expect("msm c0");
    let g1 = run_g1_pippenger_msm_gpu(&dev, &b1, &s1, 24, wb).expect("msm c1");
    assert_eq!(batched[0].to_affine(), g0.to_affine(), "circuit 0 unequal n");
    assert_eq!(batched[1].to_affine(), g1.to_affine(), "circuit 1 unequal n");
}

/// More than 128 circuits (`G1_PIPP_MAX_BATCH_CIRCUITS`) ⇒ multiple dispatches per window.
#[test]
fn g1_pippenger_batch_tiles_on_max_batch_circuits() {
    if std::env::var("CUZK_VK_SKIP_LARGE_GPU_TESTS").as_deref() == Ok("1") {
        return;
    }
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let n = 16usize;
    let wb = 8u32;
    let num_circuits = 129usize;
    let circuits_owned: Vec<(Vec<G1Affine>, Vec<Scalar>)> = (0..num_circuits)
        .map(|i| random_bases_and_scalars(n, 0xe000 + i as u64))
        .collect();
    let circuits: Vec<G1PippengerBatchItem<'_>> = circuits_owned
        .iter()
        .map(|(b, s)| G1PippengerBatchItem {
            bases: b.as_slice(),
            scalars: s.as_slice(),
        })
        .collect();
    let ns: Vec<usize> = vec![n; num_circuits];
    let tiles = pippenger_plan_batch_tiles(&ns, 1 << wb).expect("plan");
    assert!(
        tiles.len() >= 2,
        "129 circuits should require >1 tile (max batch 128), got {tiles:?}"
    );

    let batched = run_g1_pippenger_msm_gpu_batch(&dev, &circuits, wb).expect("batch tiled");
    assert_eq!(batched.len(), num_circuits);
    for i in 0..num_circuits {
        let g = run_g1_pippenger_msm_gpu(&dev, &circuits_owned[i].0, &circuits_owned[i].1, n, wb)
            .expect("serial msm");
        assert_eq!(batched[i].to_affine(), g.to_affine(), "circuit {i}");
    }
}

/// `window_bits = 16` ⇒ `bucket_count = 65536` ⇒ one circuit per dispatch (`tile_len * bucket_count` cap).
#[test]
fn g1_pippenger_batch_w16_one_circuit_per_tile() {
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let wb = 16u32;
    let n = 4usize;
    let (b0, s0) = random_bases_and_scalars(n, 0xf111);
    let (b1, s1) = random_bases_and_scalars(n, 0xf222);
    let circuits = [
        G1PippengerBatchItem {
            bases: &b0,
            scalars: &s0,
        },
        G1PippengerBatchItem {
            bases: &b1,
            scalars: &s1,
        },
    ];
    let ns = [n, n];
    let tiles = pippenger_plan_batch_tiles(&ns, 1 << wb).expect("plan");
    assert_eq!(tiles.len(), 2, "w=16 expects one circuit per tile, got {tiles:?}");

    let batched = run_g1_pippenger_msm_gpu_batch(&dev, &circuits, wb).expect("batch");
    let g0 = run_g1_pippenger_msm_gpu(&dev, &b0, &s0, n, wb).expect("c0");
    let g1 = run_g1_pippenger_msm_gpu(&dev, &b1, &s1, n, wb).expect("c1");
    assert_eq!(batched[0].to_affine(), g0.to_affine());
    assert_eq!(batched[1].to_affine(), g1.to_affine());
}

/// §8.4 **D.2** — same `h[]`-style bases, distinct scalars: GPU batch vs serial circuits.
#[test]
fn g1_pippenger_shared_bases_two_circuits_matches_serial_gpu() {
    let Some(dev) = dev_or_skip() else {
        return;
    };
    let n = 16usize;
    let wb = 8u32;
    let (bases, _) = random_bases_and_scalars(n, 0xd200);
    let (_, s0) = random_bases_and_scalars(n, 0xd201);
    let (_, s1) = random_bases_and_scalars(n, 0xd202);

    let batched = run_g1_pippenger_msm_gpu_batch_shared_bases(
        &dev,
        &bases,
        &[s0.as_slice(), s1.as_slice()],
        wb,
    )
    .expect("shared bases msm");

    let g0 = run_g1_pippenger_msm_gpu(&dev, &bases, &s0, n, wb).expect("serial 0");
    let g1 = run_g1_pippenger_msm_gpu(&dev, &bases, &s1, n, wb).expect("serial 1");
    assert_eq!(batched[0].to_affine(), g0.to_affine(), "circuit 0 shared bases");
    assert_eq!(batched[1].to_affine(), g1.to_affine(), "circuit 1 shared bases");

    let cpu =
        g1_pippenger_msm_cpu_batch_shared_bases(&bases, &[s0.as_slice(), s1.as_slice()], wb);
    assert_eq!(cpu[0].to_affine(), g0.to_affine());
    assert_eq!(cpu[1].to_affine(), g1.to_affine());
}
