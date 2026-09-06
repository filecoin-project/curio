//! GPU toy NTT (mod 998244353) vs CPU reference. Skipped unless `CUZK_VK_SKIP_SMOKE=0`.

use rand::Rng;
use rand::SeedableRng;

use cuzk_vk::device::VulkanDevice;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}
use cuzk_vk::toy_ntt::{ntt_forward_8, TOY_MOD};
use cuzk_vk::toy_ntt_gpu::run_toy_ntt8_gpu;

#[test]
fn toy_ntt_gpu_matches_cpu_when_vulkan_available() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = rand_chacha::ChaCha8Rng::from_seed([3u8; 32]);
    let mut data = [0u32; 8];
    for x in &mut data {
        *x = rng.gen::<u32>() % TOY_MOD;
    }
    let mut cpu = data;
    ntt_forward_8(&mut cpu);
    let mut gpu = data;
    run_toy_ntt8_gpu(&dev, &mut gpu).expect("toy NTT GPU");
    assert_eq!(gpu, cpu, "gpu {:?} cpu {:?}", gpu, cpu);
}
