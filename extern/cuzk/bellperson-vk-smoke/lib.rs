//! Workspace-only crate so **`cargo test -p bellperson-vk-smoke`** typechecks the forked
//! `bellperson` stack with **`vulkan-cuzk`** enabled (pulls in `cuzk-vk` via optional dependency).
//! This avoids building `bellperson` in isolation without `[patch.crates-io]` (registry edition pins).
//!
//! **Milestone B:** combined bellperson Groth16 verify + Vulkan partition smoke lives in
//! `cuzk-vk` as **`tests/milestone_b_bellperson_vulkan_smoke.rs`** (not this crate — it stays a thin link check).

#[cfg(test)]
mod tests {
    use bellperson::groth16::vulkan_cuzk::{
        prove_groth16_partition, VkGroth16Job, VkProofKind, VkProverContext, VulkanDevice,
    };
    use std::sync::Arc;

    #[test]
    fn vulkan_cuzk_module_and_cuzk_vk_symbols_resolve() {
        let _ = std::mem::size_of::<VkProofKind>();
        let _ = std::mem::size_of::<VkGroth16Job>();
        let _ = std::mem::size_of::<VkProverContext>();
        let _ = prove_groth16_partition as fn(_, _) -> _;
    }

    #[test]
    fn vulkan_cuzk_prove_partition_runs_when_icd_present() {
        if !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0")) {
            return;
        }
        let dev = match VulkanDevice::new() {
            Ok(d) => Arc::new(d),
            Err(_) => return,
        };
        let ctx = VkProverContext::new(dev);
        let job = VkGroth16Job {
            kind: VkProofKind::PoRepC2,
            circuit_log_n: 4,
            partition_index: None,
            witness_ntt_coeffs: None,
        };
        let t = prove_groth16_partition(&ctx, &job).expect("partition smoke");
        assert!(t.total.as_nanos() > 0);
    }
}
