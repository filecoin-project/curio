//! G2 Jacobian add on GPU vs `blstrs`.

use blstrs::{G2Affine, G2Projective};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::ec::{g2_jacobian_limbs_from_projective, g2_projective_from_jacobian_limbs};
use cuzk_vk::g2_ec_gpu::run_g2_jacobian_add_gpu;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn g2_jacobian_add_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xD1u8; 32]);
    for _ in 0..32 {
        let a = G2Projective::random(&mut rng);
        let b = G2Projective::random(&mut rng);
        let want = G2Affine::from(a + b);
        let ja = g2_jacobian_limbs_from_projective(&a);
        let jb = g2_jacobian_limbs_from_projective(&b);
        let jout = run_g2_jacobian_add_gpu(&dev, &ja, &jb).expect("gpu g2 jac add");
        let got = G2Affine::from(g2_projective_from_jacobian_limbs(&jout));
        assert_eq!(got, want);
    }
    let a = G2Projective::random(&mut rng);
    let want_d = G2Affine::from(a + a);
    let ja = g2_jacobian_limbs_from_projective(&a);
    let jout = run_g2_jacobian_add_gpu(&dev, &ja, &ja).expect("gpu g2 jac dbl");
    let got_d = G2Affine::from(g2_projective_from_jacobian_limbs(&jout));
    assert_eq!(got_d, want_d);
}
