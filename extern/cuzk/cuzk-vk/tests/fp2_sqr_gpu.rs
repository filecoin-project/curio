//! Fp2 square on GPU vs CPU.

use blstrs::Fp2;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fp2_gpu::run_fp2_sqr_mod_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fp2_sqr_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xB4u8; 32]);
    for _ in 0..32 {
        let a = Fp2::random(&mut rng);
        let want = a.square();
        let got = run_fp2_sqr_mod_gpu(&dev, &a).expect("gpu fp2 sqr");
        assert_eq!(got, want);
    }
    let o = Fp2::ONE;
    assert_eq!(run_fp2_sqr_mod_gpu(&dev, &o).unwrap(), o);
}
