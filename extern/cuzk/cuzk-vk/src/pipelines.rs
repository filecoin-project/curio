//! Compute pipeline helpers and sizing utilities.
//!
//! One-shot dispatch lives in [`crate::vk_oneshot`]. [`crate::VulkanDevice`] owns a `VkPipelineCache`
//! for compute pipeline creation; optional **`CUZK_VK_PIPELINE_CACHE`** load + save helpers live in
//! [`crate::device`]. **C.1 remainder:** merge across ICD versions, CI artifact wiring.

/// Conservative storage-buffer range alignment for dynamic offsets / small buffers.
pub const STORAGE_BUFFER_RANGE_ALIGN: u64 = 256;

#[inline]
pub const fn align_up_u64(value: u64, align: u64) -> u64 {
    debug_assert!(align.is_power_of_two());
    (value + align - 1) & !(align - 1)
}

/// Reserved for future pooled pipelines / on-disk cache metadata (§C.1 remainder).
#[derive(Debug, Default)]
pub struct ComputePipelineCache;
