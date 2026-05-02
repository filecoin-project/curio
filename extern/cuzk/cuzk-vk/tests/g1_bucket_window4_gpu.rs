//! Single-window 4-bit bucket sum on GPU vs digit-wise CPU reference.

use blstrs::{G1Affine, G1Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::g1_msm_bucket_gpu::{g1_bucket_window_msm_cpu, run_g1_bucket_sum_window4_gpu};
use ff::Field;
use group::{Curve, Group};
use rand::Rng;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

fn vulkan_device_for_smoke() -> Option<VulkanDevice> {
    if skip_vulkan_smoke() {
        return None;
    }
    match VulkanDevice::new() {
        Ok(d) => Some(d),
        Err(e) => {
            eprintln!(
                "skip: Vulkan init failed with CUZK_VK_SKIP_SMOKE=0 ({e:#})\n\
                 hint (macOS): install loader + MoltenVK (e.g. brew) and use extern/cuzk/apple-m2-vulkan-smoke.sh, or set DYLD_FALLBACK_LIBRARY_PATH to find libvulkan.dylib."
            );
            None
        }
    }
}

#[test]
fn g1_bucket_window4_matches_cpu() {
    let Some(dev) = vulkan_device_for_smoke() else {
        return;
    };
    let mut rng = ChaCha8Rng::from_seed([0xB3u8; 32]);
    for n in [1usize, 7, 16, 32] {
        let bases: Vec<G1Affine> = (0..n)
            .map(|_| (G1Projective::generator() * Scalar::random(&mut rng)).to_affine())
            .collect();
        let digits: Vec<u32> = (0..n).map(|_| rng.gen::<u32>() % 16).collect();
        let want = g1_bucket_window_msm_cpu(&bases, &digits, n);
        let got = run_g1_bucket_sum_window4_gpu(&dev, &bases, &digits, n).expect("gpu bucket");
        assert_eq!(got, want, "n={n}");
    }
}
