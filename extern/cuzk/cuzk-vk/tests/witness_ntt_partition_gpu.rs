//! **B₂ — witness → Vulkan:** [`VkGroth16Job::witness_ntt_coeffs`] drives the Fr NTT general
//! round-trip in [`cuzk_vk::prove_groth16_partition`] (Vulkan smoke only).

use std::sync::Arc;

use blstrs::Scalar;
use cuzk_vk::{
    prove_groth16_partition, VkGroth16Job, VkProofKind, VkProverContext, VulkanDevice,
};

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn partition_smoke_accepts_witness_ntt_coeffs() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = Arc::new(VulkanDevice::new().expect("Vulkan init"));
    let ctx = VkProverContext::new(dev);
    let lg = 3u32;
    let n = 1usize << lg;
    let witness_ntt_coeffs: Vec<Scalar> = (0..n)
        .map(|i| Scalar::from(5000u64 + i as u64))
        .collect();
    let job = VkGroth16Job {
        kind: VkProofKind::WindowPost,
        circuit_log_n: lg,
        partition_index: Some(0),
        witness_ntt_coeffs: Some(witness_ntt_coeffs),
    };
    let t = prove_groth16_partition(&ctx, &job).expect("partition with witness NTT coeffs");
    assert!(t.fr_ntt_gpu.as_nanos() > 0);
}

#[test]
fn partition_smoke_rejects_witness_ntt_wrong_length() {
    if skip_vulkan_smoke() {
        return;
    }
    let dev = Arc::new(VulkanDevice::new().expect("Vulkan init"));
    let ctx = VkProverContext::new(dev);
    let job = VkGroth16Job {
        kind: VkProofKind::PoRepC2,
        circuit_log_n: 4,
        partition_index: None,
        witness_ntt_coeffs: Some(vec![Scalar::from(1u64); 4]),
    };
    assert!(
        prove_groth16_partition(&ctx, &job).is_err(),
        "expected error when witness len != 2^log_n"
    );
}
