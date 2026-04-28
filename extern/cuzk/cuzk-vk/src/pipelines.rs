//! Compute pipeline helpers and sizing utilities.
//!
//! One-shot dispatch lives in [`crate::vk_oneshot`]; disk-backed `VkPipelineCache` is roadmap §C.1.

/// Conservative storage-buffer range alignment for dynamic offsets / small buffers.
pub const STORAGE_BUFFER_RANGE_ALIGN: u64 = 256;

#[inline]
pub const fn align_up_u64(value: u64, align: u64) -> u64 {
    debug_assert!(align.is_power_of_two());
    (value + align - 1) & !(align - 1)
}

/// Placeholder until shared pipeline cache lands (`VkPipelineCache` on disk — roadmap C.1).
#[derive(Debug, Default)]
pub struct ComputePipelineCache;
