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

/// Little-endian **four bytes** for one `uint` GLSL specialization constant (`layout(constant_id = N) const uint …`).
///
/// **Milestone B₂ §8.3 C.3:** internal `vk_oneshot` compute paths take optional specialization data; smoke in [`crate::spec_constant_smoke_gpu`];
/// production-shaped use in [`crate::msm_gpu::run_msm_dispatch_hitcount_smoke`] (workgroup `local_size_x` = **SpecId 0**).
/// **Remainder:** Fr NTT / bucket MSM tails still naga-compiled (`constant_id` / `local_size_x_id` unsupported there).
#[inline]
pub const fn spec_constant_u32_le(val: u32) -> [u8; 4] {
    val.to_le_bytes()
}

#[cfg(test)]
mod spec_tests {
    use super::*;

    #[test]
    fn spec_constant_u32_le_roundtrip() {
        let v = 0x0a0b0c0du32;
        assert_eq!(u32::from_le_bytes(spec_constant_u32_le(v)), v);
    }
}
