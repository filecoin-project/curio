//! Fr modular subtract on GPU vs CPU.

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fr_gpu::run_fr_sub_mod_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fr_sub_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x5Bu8; 32]);
    for _ in 0..48 {
        let a = Scalar::random(&mut rng);
        let b = Scalar::random(&mut rng);
        let want = a - b;
        let got = run_fr_sub_mod_gpu(&dev, &a, &b).expect("gpu");
        assert_eq!(got, want, "a={a:?} b={b:?}");
    }
}
