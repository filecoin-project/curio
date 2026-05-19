//! Fp2 add on GPU vs CPU.

use blstrs::Fp2;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fp2_gpu::run_fp2_add_mod_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fp2_add_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xB1u8; 32]);
    for _ in 0..32 {
        let a = Fp2::random(&mut rng);
        let b = Fp2::random(&mut rng);
        let want = a + b;
        let got = run_fp2_add_mod_gpu(&dev, &a, &b).expect("gpu fp2 add");
        assert_eq!(got, want);
    }
    let z = Fp2::ZERO;
    let o = Fp2::ONE;
    assert_eq!(run_fp2_add_mod_gpu(&dev, &z, &o).unwrap(), o);
}
