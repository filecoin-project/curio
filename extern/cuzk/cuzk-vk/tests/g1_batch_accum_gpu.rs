//! Phase G: bitmap batch sum of affine G1 points (GPU vs CPU).

use blstrs::{G1Affine, G1Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::ec::g1_projective_from_jacobian_limbs;
use cuzk_vk::g1_batch_gpu::{
    g1_batch_jacobian_accum_bitmap_cpu, run_g1_batch_jacobian_accum_bitmap_gpu,
};
use ff::Field;
use group::Group;
use rand::RngCore;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn g1_batch_accum_bitmap_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xB1u8; 32]);
    // Smoke check on n=2 with bitmap=0b11 to exercise the per-thread "two non-identity adds"
    // path (thread 0 processes both points sequentially, no reduction across threads).
    {
        let pts: Vec<G1Affine> = (0..2)
            .map(|_| G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)))
            .collect();
        let cpu = g1_batch_jacobian_accum_bitmap_cpu(2, 0b11, &pts);
        let gpu = run_g1_batch_jacobian_accum_bitmap_gpu(&dev, 2, 0b11, &pts).expect("n=2");
        let cpu_aff = G1Affine::from(g1_projective_from_jacobian_limbs(&cpu));
        let gpu_aff = G1Affine::from(g1_projective_from_jacobian_limbs(&gpu));
        assert_eq!(gpu_aff, cpu_aff, "n=2 sanity (single-thread two-add)");
    }
    for n in [1usize, 7, 16, 32, 64] {
        let points: Vec<G1Affine> = (0..n)
            .map(|_| G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)))
            .collect();
        for _ in 0..8 {
            let bitmap = rng.next_u64();
            let cpu_jac = g1_batch_jacobian_accum_bitmap_cpu(n, bitmap, &points);
            let gpu_jac = run_g1_batch_jacobian_accum_bitmap_gpu(&dev, n, bitmap, &points)
                .expect("g1 batch accum");
            // The GPU and CPU paths can produce different Jacobian limbs even for the same
            // projective point (different `Z` factors). Compare via affine normalisation.
            let cpu_aff = G1Affine::from(g1_projective_from_jacobian_limbs(&cpu_jac));
            let gpu_aff = G1Affine::from(g1_projective_from_jacobian_limbs(&gpu_jac));
            assert_eq!(gpu_aff, cpu_aff, "n={} bitmap={:064b}", n, bitmap);
        }
    }
}
