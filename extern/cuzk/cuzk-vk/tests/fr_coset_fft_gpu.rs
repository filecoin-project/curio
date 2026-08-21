//! Phase F — GPU coset forward FFT ([`cuzk_vk::fr_coset_gpu::run_fr_coset_fft_forward_gpu`]) matches bellperson
//! [`EvaluationDomain::coset_fft`] when Vulkan smoke is enabled.

use bellperson::domain::EvaluationDomain;
use blstrs::Scalar;
use cuzk_vk::device::VulkanDevice;
use cuzk_vk::fr_coset_gpu::{run_fr_coset_fft_forward_gpu, run_fr_distribute_powers_gpu};
use cuzk_vk::fr_ntt_general_gpu::{
    run_fr_ntt_general_forward_gpu, run_fr_ntt_general_forward_gpu_distribute,
    run_fr_ntt_radix8_forward_gpu, run_fr_ntt_radix8_forward_gpu_distribute,
};
use ec_gpu_gen::threadpool::Worker;
use ff::{Field, PrimeField};
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn coset_fft_gpu_matches_bellperson_domain() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x67u8; 32]);
    let n = 4096usize;
    let coeffs: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    let worker = Worker::new();
    let mut domain = EvaluationDomain::from_coeffs(coeffs.clone()).expect("domain");
    domain.coset_fft(&worker, &mut None).expect("coset_fft");
    let want: Vec<Scalar> = domain.into_coeffs();

    let got = run_fr_coset_fft_forward_gpu(&dev, &coeffs).expect("gpu coset fft");
    assert_eq!(got, want);
}

#[test]
fn coset_forward_fused_pack_matches_pointwise_then_ntt() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = VulkanDevice::new().expect("Vulkan init");
    let mut rng = ChaCha8Rng::from_seed([0x71u8; 32]);
    let n = 512usize;
    let coeffs: Vec<Scalar> = (0..n).map(|_| Scalar::random(&mut rng)).collect();
    let shifted = run_fr_distribute_powers_gpu(&dev, &coeffs, Scalar::MULTIPLICATIVE_GENERATOR)
        .expect("distribute gpu");
    let two_step = if n >= 8 {
        run_fr_ntt_radix8_forward_gpu(&dev, &shifted).expect("ntt radix8")
    } else {
        run_fr_ntt_general_forward_gpu(&dev, &shifted).expect("ntt radix4")
    };
    assert_eq!(
        two_step,
        run_fr_coset_fft_forward_gpu(&dev, &coeffs).expect("coset entrypoint")
    );
    assert_eq!(
        two_step,
        run_fr_ntt_general_forward_gpu_distribute(&dev, &coeffs, Scalar::MULTIPLICATIVE_GENERATOR)
            .expect("pack-fused radix4")
    );
    if n >= 8 {
        assert_eq!(
            two_step,
            run_fr_ntt_radix8_forward_gpu_distribute(
                &dev,
                &coeffs,
                Scalar::MULTIPLICATIVE_GENERATOR,
            )
            .expect("pack-fused radix8")
        );
    }
}
