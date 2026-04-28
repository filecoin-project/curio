//! G1 Jacobian add on GPU vs `blstrs` projective law.

use blstrs::{G1Affine, G1Projective};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::ec::{g1_jacobian_limbs_from_projective, g1_projective_from_jacobian_limbs};
use cuzk_vk::g1_ec_gpu::run_g1_jacobian_add_gpu;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn g1_jacobian_add_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xC1u8; 32]);
    for _ in 0..64 {
        let a = G1Projective::random(&mut rng);
        let b = G1Projective::random(&mut rng);
        let want = G1Affine::from(a + b);
        let ja = g1_jacobian_limbs_from_projective(&a);
        let jb = g1_jacobian_limbs_from_projective(&b);
        let jout = run_g1_jacobian_add_gpu(&dev, &ja, &jb).expect("gpu jac add");
        let got = G1Affine::from(g1_projective_from_jacobian_limbs(&jout));
        assert_eq!(got, want);
    }
    let a = G1Projective::random(&mut rng);
    let want_d = G1Affine::from(a + a);
    let ja = g1_jacobian_limbs_from_projective(&a);
    let jout = run_g1_jacobian_add_gpu(&dev, &ja, &ja).expect("gpu jac dbl");
    let got_d = G1Affine::from(g1_projective_from_jacobian_limbs(&jout));
    assert_eq!(got_d, want_d);
}
