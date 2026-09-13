//! G1 XYZZ mixed add on GPU vs `blstrs`.

use blstrs::{G1Affine, G1Projective};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::ec::{g1_affine_from_xyzz_limbs, g1_xyzz_limbs_from_projective, G1XyzzLimbs};
use cuzk_vk::g1::BLS12_381_FP_U32_LIMBS;
use cuzk_vk::g1_ec_gpu::run_g1_xyzz_add_mixed_gpu;
use group::prime::PrimeCurveAffine;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn g1_xyzz_add_mixed_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xC2u8; 32]);
    for _ in 0..64 {
        let acc = G1Projective::random(&mut rng);
        let mut xy = g1_xyzz_limbs_from_projective(&acc);
        let p2 = G1Affine::from(G1Projective::random(&mut rng));
        if bool::from(p2.is_identity()) {
            continue;
        }
        let want = G1Affine::from(acc + p2);
        run_g1_xyzz_add_mixed_gpu(&dev, &mut xy, &p2).expect("gpu xyzz add");
        let got = g1_affine_from_xyzz_limbs(&xy).expect("xyzz decode");
        assert_eq!(got, want);
    }
}

#[test]
fn g1_xyzz_add_mixed_from_infinity_accumulator() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let p2 = G1Affine::generator();
    let mut xy = G1XyzzLimbs {
        x: [0u32; BLS12_381_FP_U32_LIMBS],
        y: [0u32; BLS12_381_FP_U32_LIMBS],
        zzz: [0u32; BLS12_381_FP_U32_LIMBS],
        zz: [0u32; BLS12_381_FP_U32_LIMBS],
    };
    run_g1_xyzz_add_mixed_gpu(&dev, &mut xy, &p2).expect("gpu xyzz from inf");
    let got = g1_affine_from_xyzz_limbs(&xy).expect("decode");
    assert_eq!(got, p2);
}
