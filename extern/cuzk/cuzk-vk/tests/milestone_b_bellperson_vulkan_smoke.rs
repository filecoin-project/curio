//! **Milestone B₁ (integration)** — **§3.2 step 11:** native bellperson Groth16 **prove + verify**
//! (BLS12-381 **pairing**), then [`cuzk_vk::prove_groth16_partition`] on a live ICD (`CUZK_VK_SKIP_SMOKE=0`).
//!
//! **B₂ slice:** [`VkGroth16Job::witness_ntt_coeffs`] runs caller Fr values through the partition GPU NTT leg;
//! full bellperson R1CS into Vulkan prove remains future work — [`MILESTONE_B.md`](../../MILESTONE_B.md).

mod common;

use std::sync::Arc;

use bellperson::groth16::{
    create_random_proof, generate_random_parameters, prepare_verifying_key, verify_proof,
};
use blstrs::{Bls12, Scalar};
use ff::Field;
use rand::thread_rng;

use common::MulCircuit;
use cuzk_vk::{
    msm_config_for_device, prove_groth16_partition, VkGroth16Job, VkProofKind, VkProverContext,
    VulkanDevice,
};

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn bellperson_groth16_verify_then_vulkan_partition_smoke() {
    let mut rng = thread_rng();
    let params = generate_random_parameters::<Bls12, _, _>(
        MulCircuit {
            a: Some(Scalar::ONE),
            b: Some(Scalar::ONE),
        },
        &mut rng,
    )
    .expect("paramgen");
    let pvk = prepare_verifying_key(&params.vk);

    let a = Scalar::from(7u64);
    let b = Scalar::from(11u64);
    let proof = create_random_proof(
        MulCircuit {
            a: Some(a),
            b: Some(b),
        },
        &params,
        &mut rng,
    )
    .expect("prove");

    assert!(
        verify_proof(&pvk, &proof, &[a * b]).expect("verify"),
        "Groth16 verify (pairing) failed before Vulkan smoke"
    );

    if skip_vulkan_smoke() {
        return;
    }
    let dev = match VulkanDevice::new() {
        Ok(d) => Arc::new(d),
        Err(_) => return,
    };
    let msm = msm_config_for_device(&dev.physical_device_info());
    assert!((1..=31).contains(&msm.window_bits));

    let ctx = VkProverContext::new(dev);
    let n = 1usize << 4;
    let witness_ntt_coeffs: Vec<Scalar> = (0..n)
        .map(|i| Scalar::from(1000u64 + i as u64))
        .collect();
    let job = VkGroth16Job {
        kind: VkProofKind::PoRepC2,
        circuit_log_n: 4,
        partition_index: None,
        witness_ntt_coeffs: Some(witness_ntt_coeffs),
    };
    let t = prove_groth16_partition(&ctx, &job).expect("partition smoke after bellperson");
    assert!(t.total.as_nanos() > 0);
}
