//! Optional Vulkan proving backend via the `cuzk-vk` crate.
//!
//! Enable with **`--features vulkan-cuzk`**. This is **mutually exclusive** with
//! **`cuda-supraseal`** (compile-time `compile_error!` in `groth16/mod.rs`).
//!
//! The proving entry point here is still the native bellperson Groth16 API (`ext`); `cuzk-vk`
//! supplies Vulkan compute primitives and partition smoke (`prove_groth16_partition`) for the
//! Filecoin cuZK integration path.

pub use cuzk_vk::{
    prove_groth16_partition, VkGroth16Job, VkProofKind, VkProofTimings, VkProverContext,
    VkProverSession, VulkanDevice,
};
