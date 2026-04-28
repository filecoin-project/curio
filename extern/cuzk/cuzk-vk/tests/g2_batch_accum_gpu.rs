//! Phase G: G2 bitmap batch Jacobian sum (GPU vs CPU).

use blstrs::{G2Affine, G2Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::g2_batch_gpu::{
    g2_batch_jacobian_accum_bitmap_cpu, run_g2_batch_jacobian_accum_bitmap_gpu,
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
fn g2_batch_accum_bitmap_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xB2u8; 32]);
    for n in [1usize, 5, 16] {
        let points: Vec<G2Affine> = (0..n)
            .map(|_| G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)))
            .collect();
        for _ in 0..8 {
            let bitmap = rng.next_u32();
            let cpu = g2_batch_jacobian_accum_bitmap_cpu(n, bitmap, &points);
            let gpu = run_g2_batch_jacobian_accum_bitmap_gpu(&dev, n, bitmap, &points)
                .expect("g2 batch accum");
            assert_eq!(gpu, cpu, "n={} bitmap={:016b}", n, bitmap);
        }
    }
}
