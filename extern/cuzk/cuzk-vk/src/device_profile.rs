//! Host-side **MSM window** hints (roadmap **§8.1 A.1**) until per-GPU VALU profiling exists.
//!
//! Override with **`CUZK_VK_MSM_WINDOW_BITS`** (decimal `1..=31`) for experiments; invalid values are ignored.

use crate::device::PhysicalDeviceInfo;
use crate::msm::MsmConfig;

/// PCI-style vendor ids (subset; unknown vendors fall back to `16`).
pub mod vendor {
    pub const AMD: u32 = 0x1002;
    pub const NVIDIA: u32 = 0x10de;
    /// Common for Apple / MoltenVK paths (not guaranteed on all builds).
    pub const APPLE: u32 = 0x106b;
}

/// Default Fr MSM window size hint from GPU vendor (unsigned Pippenger-style buckets = `2^w` rows).
#[inline]
pub fn recommended_msm_window_bits(vendor_id: u32) -> u32 {
    match vendor_id {
        vendor::APPLE => 12,
        vendor::AMD | vendor::NVIDIA => 16,
        _ => 16,
    }
}

/// [`MsmConfig`] with `window_bits` from env or [`recommended_msm_window_bits`].
pub fn msm_config_for_device(info: &PhysicalDeviceInfo) -> MsmConfig {
    let window_bits = std::env::var("CUZK_VK_MSM_WINDOW_BITS")
        .ok()
        .and_then(|s| s.parse::<u32>().ok())
        .filter(|&w| (1..=31).contains(&w))
        .unwrap_or_else(|| recommended_msm_window_bits(info.vendor_id));
    MsmConfig {
        window_bits,
        batch_circuits: 1,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn recommended_bits_by_vendor() {
        assert_eq!(recommended_msm_window_bits(vendor::AMD), 16);
        assert_eq!(recommended_msm_window_bits(vendor::NVIDIA), 16);
        assert_eq!(recommended_msm_window_bits(vendor::APPLE), 12);
        assert_eq!(recommended_msm_window_bits(0xffff), 16);
    }

}
