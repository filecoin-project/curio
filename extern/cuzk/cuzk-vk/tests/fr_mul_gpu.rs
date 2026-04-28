//! Fr modular multiply on GPU vs CPU (double-and-add shader).

use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fr_gpu::run_fr_mul_mod_gpu;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn fr_mul_gpu_matches_cpu() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0xE7u8; 32]);
    for _ in 0..16 {
        let a = Scalar::random(&mut rng);
        let b = Scalar::random(&mut rng);
        let want = a * b;
        let got = run_fr_mul_mod_gpu(&dev, &a, &b).expect("gpu");
        assert_eq!(got, want, "a={a:?} b={b:?}");
    }
    assert_eq!(
        run_fr_mul_mod_gpu(&dev, &Scalar::ZERO, &Scalar::ONE).unwrap(),
        Scalar::ZERO
    );
    assert_eq!(
        run_fr_mul_mod_gpu(&dev, &Scalar::ONE, &Scalar::ONE).unwrap(),
        Scalar::ONE
    );
}
