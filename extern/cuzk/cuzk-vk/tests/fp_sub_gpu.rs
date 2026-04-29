//! Fp modular sub on GPU vs CPU.

use blstrs::Fp;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fp_gpu::run_fp_sub_mod_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fp_sub_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xA2u8; 32]);
    for _ in 0..32 {
        let a = Fp::random(&mut rng);
        let b = Fp::random(&mut rng);
        let want = a - b;
        let got = run_fp_sub_mod_gpu(&dev, &a, &b).expect("gpu fp sub");
        assert_eq!(got, want);
    }
}
