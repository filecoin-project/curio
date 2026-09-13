//! `n = 8` Fr NTT on GPU vs CPU (forward, inverse, round-trip).

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fr_ntt_gpu::{
    run_fr_ntt8_forward_gpu, run_fr_ntt8_inverse_gpu, run_fr_ntt8_roundtrip_gpu,
};
use cuzk_vk::{fr_intt_inplace, fr_ntt_inplace, fr_omega};
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fr_ntt8_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let omega = fr_omega(8);
    let mut rng = ChaCha8Rng::from_seed([0x4Eu8; 32]);
    for _ in 0..32 {
        let input: [Scalar; 8] = std::array::from_fn(|_| Scalar::random(&mut rng));
        let mut want = input;
        fr_ntt_inplace(&mut want[..], omega);
        let got = run_fr_ntt8_forward_gpu(&dev, &input).expect("gpu ntt8");
        assert_eq!(got, want, "input={input:?}");
    }
    let zeros = [Scalar::ZERO; 8];
    assert_eq!(
        run_fr_ntt8_forward_gpu(&dev, &zeros).unwrap(),
        zeros
    );
}

#[test]
fn fr_ntt8_inverse_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let omega = fr_omega(8);
    let mut rng = ChaCha8Rng::from_seed([0x71u8; 32]);
    for _ in 0..32 {
        let input: [Scalar; 8] = std::array::from_fn(|_| Scalar::random(&mut rng));
        let mut evals = input;
        fr_ntt_inplace(&mut evals[..], omega);
        let mut want = evals;
        fr_intt_inplace(&mut want[..], omega);
        assert_eq!(want, input);
        let got = run_fr_ntt8_inverse_gpu(&dev, &evals).expect("gpu intt8");
        assert_eq!(got, input, "evals={evals:?}");
    }
}

#[test]
fn fr_ntt8_gpu_roundtrip_matches_input() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x72u8; 32]);
    for _ in 0..16 {
        let input: [Scalar; 8] = std::array::from_fn(|_| Scalar::random(&mut rng));
        let got = run_fr_ntt8_roundtrip_gpu(&dev, &input).expect("gpu roundtrip");
        assert_eq!(got, input);
    }
}
