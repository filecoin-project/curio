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
    for i in 0..64 {
        let a = G1Projective::random(&mut rng);
        let b = G1Projective::random(&mut rng);
        let want = G1Affine::from(a + b);
        let ja = g1_jacobian_limbs_from_projective(&a);
        let jb = g1_jacobian_limbs_from_projective(&b);
        let jout = run_g1_jacobian_add_gpu(&dev, &ja, &jb).expect("gpu jac add");
        let got = G1Affine::from(g1_projective_from_jacobian_limbs(&jout));
        assert_eq!(got, want, "iter {i}");
    }
    // Doubling case: the host dispatcher routes `a == b` to the dedicated
    // `g1_jacobian_dbl108.spv` kernel, so this exercises that path end-to-end.
    let a = G1Projective::random(&mut rng);
    let want_d = G1Affine::from(a + a);
    let ja = g1_jacobian_limbs_from_projective(&a);
    let jout = run_g1_jacobian_add_gpu(&dev, &ja, &ja).expect("gpu jac dbl");
    let got_d = G1Affine::from(g1_projective_from_jacobian_limbs(&jout));
    assert_eq!(got_d, want_d, "doubling case");
    // Identity inputs: the host short-circuits without touching the GPU; ensure
    // both `a + 0` and `0 + b` return the non-identity operand verbatim.
    let zero = cuzk_vk::ec::G1JacobianLimbs::zero();
    let a = G1Projective::random(&mut rng);
    let ja = g1_jacobian_limbs_from_projective(&a);
    let r = run_g1_jacobian_add_gpu(&dev, &ja, &zero).expect("gpu a+0");
    assert_eq!(r.x, ja.x);
    assert_eq!(r.y, ja.y);
    assert_eq!(r.z, ja.z);
    let r = run_g1_jacobian_add_gpu(&dev, &zero, &ja).expect("gpu 0+a");
    assert_eq!(r.x, ja.x);
    assert_eq!(r.y, ja.y);
    assert_eq!(r.z, ja.z);
}

/// Affine + Affine via the Jacobian shader: both inputs have `Z = 1` (Mont form), which exercises
/// only the addition branch with trivial Z handling. If this passes but the random-`Z` test fails,
/// the bug is in the H/Z handling.
#[test]
fn g1_jacobian_add_gpu_affine_inputs() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xC2u8; 32]);
    for i in 0..16 {
        let a_aff = blstrs::G1Affine::from(G1Projective::random(&mut rng));
        let b_aff = blstrs::G1Affine::from(G1Projective::random(&mut rng));
        let a = G1Projective::from(a_aff);
        let b = G1Projective::from(b_aff);
        let want = G1Affine::from(a + b);
        let ja = g1_jacobian_limbs_from_projective(&a);
        let jb = g1_jacobian_limbs_from_projective(&b);
        let jout = run_g1_jacobian_add_gpu(&dev, &ja, &jb).expect("gpu jac add");
        let got = G1Affine::from(g1_projective_from_jacobian_limbs(&jout));
        assert_eq!(got, want, "iter {i} (affine inputs, Z=1)");
    }
}
