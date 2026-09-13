//! G2 XYZZ mixed add on GPU vs `blstrs`.

use blstrs::{G2Affine, G2Projective};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::ec::{g2_affine_from_xyzz_limbs, g2_xyzz_limbs_from_projective, G2XyzzLimbs};
use cuzk_vk::fp2::BLS12_381_FP2_U32_LIMBS;
use cuzk_vk::g2_ec_gpu::run_g2_xyzz_add_mixed_gpu;
use group::prime::PrimeCurveAffine;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn g2_xyzz_add_mixed_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xD2u8; 32]);
    for _ in 0..32 {
        let acc = G2Projective::random(&mut rng);
        let mut xy = g2_xyzz_limbs_from_projective(&acc);
        let p2 = G2Affine::from(G2Projective::random(&mut rng));
        if bool::from(p2.is_identity()) {
            continue;
        }
        let want = G2Affine::from(acc + p2);
        run_g2_xyzz_add_mixed_gpu(&dev, &mut xy, &p2).expect("gpu g2 xyzz");
        let got = g2_affine_from_xyzz_limbs(&xy).expect("decode");
        assert_eq!(got, want);
    }
}

#[test]
fn g2_xyzz_add_mixed_from_infinity_accumulator() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let p2 = G2Affine::generator();
    let z = [0u32; BLS12_381_FP2_U32_LIMBS];
    let mut xy = G2XyzzLimbs { x: z, y: z, zzz: z, zz: z };
    run_g2_xyzz_add_mixed_gpu(&dev, &mut xy, &p2).expect("gpu g2 xyzz from inf");
    let got = g2_affine_from_xyzz_limbs(&xy).expect("decode");
    assert_eq!(got, p2);
}
