//! Fp2 sub on GPU vs CPU.

use blstrs::Fp2;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fp2_gpu::run_fp2_sub_mod_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fp2_sub_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xB2u8; 32]);
    for _ in 0..32 {
        let a = Fp2::random(&mut rng);
        let b = Fp2::random(&mut rng);
        let want = a - b;
        let got = run_fp2_sub_mod_gpu(&dev, &a, &b).expect("gpu fp2 sub");
        assert_eq!(got, want);
    }
}
