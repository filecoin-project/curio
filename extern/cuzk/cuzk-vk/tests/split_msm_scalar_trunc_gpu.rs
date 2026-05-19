//! B₂ §8.1: [`Scalar`] → truncated u64 → same GPU bit-plane MSM as `split_msm_g1_bitsplit_gpu`.

use blstrs::{G1Affine, G1Projective, Scalar};
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::split_msm::{
    g1_msm_bitplanes_scalars_trunc_gpu_host, g1_msm_scalars_trunc_reference, g1_msm_small_u64_agrees_with_multi_exp,
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
fn split_msm_scalar_trunc_reference_matches_multi_exp() {
    let mut rng = ChaCha8Rng::from_seed([0x51u8; 32]);
    let max_bits = 22u32;
    let cap = (1u64 << max_bits) - 1;
    let n = 17usize;
    let bases: Vec<G1Affine> = (0..n)
        .map(|_| G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)))
        .collect();
    let scalars: Vec<Scalar> = (0..n)
        .map(|_| {
            let k = (rng.next_u64() ^ rng.next_u64()) & cap;
            Scalar::from(k)
        })
        .collect();
    let u64s: Vec<u64> = scalars.iter().map(|s| {
        let b = s.to_bytes_le();
        u64::from_le_bytes(b[..8].try_into().unwrap()) & cap
    }).collect();
    assert!(g1_msm_small_u64_agrees_with_multi_exp(&bases, &u64s, n, max_bits));
    let ref_trunc = g1_msm_scalars_trunc_reference(&bases, &scalars, n, max_bits);
    let pts: Vec<G1Projective> = (0..n).map(|i| G1Projective::from(bases[i])).collect();
    let sc: Vec<Scalar> = (0..n).map(|i| Scalar::from(u64s[i])).collect();
    assert_eq!(ref_trunc, G1Projective::multi_exp(&pts, &sc));
}

#[test]
fn split_msm_scalar_trunc_gpu_matches_reference() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x52u8; 32]);
    let max_bits = 19u32;
    let cap = (1u64 << max_bits) - 1;
    for n in [3usize, 24, 64] {
        let bases: Vec<G1Affine> = (0..n)
            .map(|_| G1Affine::from(G1Projective::generator() * Scalar::random(&mut rng)))
            .collect();
        let scalars: Vec<Scalar> = (0..n)
            .map(|_| Scalar::from((rng.next_u64() ^ rng.next_u64()) & cap))
            .collect();
        let want = g1_msm_scalars_trunc_reference(&bases, &scalars, n, max_bits);
        let got =
            g1_msm_bitplanes_scalars_trunc_gpu_host(&dev, &bases, &scalars, n, max_bits).expect("gpu");
        assert_eq!(got, want, "n={n}");
    }
}
