//! General-size Fr NTT on GPU vs CPU [`cuzk_vk::ntt`].

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fr_ntt_general_gpu::{
    run_fr_ntt_general_forward_gpu, run_fr_ntt_general_inverse_gpu,
    run_fr_ntt_general_roundtrip_gpu,
};
use cuzk_vk::ntt::{fr_intt_inplace, fr_ntt_inplace, fr_omega};
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fr_ntt_general_forward_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x4Eu8; 32]);
    for log_n in [3u32, 6, 10, 14] {
        let n = 1usize << log_n;
        let omega = fr_omega(n);
        let v: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
        let gpu = run_fr_ntt_general_forward_gpu(&dev, &v).expect("gpu forward ntt");
        let mut cpu = v.clone();
        fr_ntt_inplace(&mut cpu, omega);
        assert_eq!(gpu, cpu, "forward log_n={}", log_n);
    }
}

#[test]
fn fr_ntt_general_inverse_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x51u8; 32]);
    for log_n in [3u32, 6, 10, 14] {
        let n = 1usize << log_n;
        let omega = fr_omega(n);
        let mut evals: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
        fr_ntt_inplace(&mut evals, omega);
        let gpu = run_fr_ntt_general_inverse_gpu(&dev, &evals).expect("gpu inverse ntt");
        let mut cpu = evals.clone();
        fr_intt_inplace(&mut cpu, omega);
        assert_eq!(gpu, cpu, "inverse log_n={}", log_n);
    }
}

#[test]
fn fr_ntt_general_roundtrip_random() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x52u8; 32]);
    for log_n in [3u32, 6, 10, 14] {
        let n = 1usize << log_n;
        let v: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
        let back = run_fr_ntt_general_roundtrip_gpu(&dev, &v).expect("gpu roundtrip");
        assert_eq!(back, v, "roundtrip log_n={}", log_n);
    }
}

#[test]
fn fr_ntt_general_empty_is_noop() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let a: Vec<Scalar> = vec![];
    assert!(run_fr_ntt_general_forward_gpu(&dev, &a).unwrap().is_empty());
    assert!(run_fr_ntt_general_inverse_gpu(&dev, &a).unwrap().is_empty());
}
