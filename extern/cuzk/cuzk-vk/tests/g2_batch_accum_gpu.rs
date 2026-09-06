//! Phase G: G2 bitmap batch Jacobian sum (GPU vs CPU).

use blstrs::{G2Affine, G2Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::ec::g2_projective_from_jacobian_limbs;
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
    {
        let pts: Vec<G2Affine> = (0..2)
            .map(|_| G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)))
            .collect();
        // n=1 with bit 0 = 1 → exercise the "single non-identity add" path.
        let cpu = g2_batch_jacobian_accum_bitmap_cpu(1, 0b1, &pts);
        let gpu = run_g2_batch_jacobian_accum_bitmap_gpu(&dev, 1, 0b1, &pts).expect("n=1 b1");
        eprintln!("--- n=1 bit0=1 ---");
        eprintln!("  gpu = {:08x?} | {:08x?} | {:08x?}", gpu.x, gpu.y, gpu.z);
        eprintln!("  cpu = {:08x?} | {:08x?} | {:08x?}", cpu.x, cpu.y, cpu.z);
        assert_eq!(
            G2Affine::from(g2_projective_from_jacobian_limbs(&cpu)),
            G2Affine::from(g2_projective_from_jacobian_limbs(&gpu)),
            "n=1 bit0=1"
        );
        let cpu = g2_batch_jacobian_accum_bitmap_cpu(2, 0b11, &pts);
        let gpu = run_g2_batch_jacobian_accum_bitmap_gpu(&dev, 2, 0b11, &pts).expect("n=2");
        eprintln!("--- n=2 sanity ---");
        eprintln!("  gpu = {:08x?} | {:08x?} | {:08x?}", gpu.x, gpu.y, gpu.z);
        eprintln!("  cpu = {:08x?} | {:08x?} | {:08x?}", cpu.x, cpu.y, cpu.z);
        assert_eq!(
            G2Affine::from(g2_projective_from_jacobian_limbs(&cpu)),
            G2Affine::from(g2_projective_from_jacobian_limbs(&gpu)),
            "n=2 sanity"
        );
    }
    for n in [1usize, 5, 16] {
        let points: Vec<G2Affine> = (0..n)
            .map(|_| G2Affine::from(G2Projective::generator() * Scalar::random(&mut rng)))
            .collect();
        for _ in 0..8 {
            let bitmap = rng.next_u32();
            let cpu_jac = g2_batch_jacobian_accum_bitmap_cpu(n, bitmap, &points);
            let gpu_jac = run_g2_batch_jacobian_accum_bitmap_gpu(&dev, n, bitmap, &points)
                .expect("g2 batch accum");
            // GPU and CPU paths can produce different Jacobian limbs (different `Z`) for
            // the same projective point. Compare via affine normalisation.
            let cpu_aff = G2Affine::from(g2_projective_from_jacobian_limbs(&cpu_jac));
            let gpu_aff = G2Affine::from(g2_projective_from_jacobian_limbs(&gpu_jac));
            assert_eq!(gpu_aff, cpu_aff, "n={} bitmap={:016b}", n, bitmap);
        }
    }
}
