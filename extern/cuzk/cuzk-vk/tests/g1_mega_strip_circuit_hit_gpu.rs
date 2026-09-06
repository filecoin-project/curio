//! Mega-strip `(circuit_id, tid_in_strip)` map vs host grid model.

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::g1_mega_strip_gpu::run_g1_mega_strip_circuit_hit_gpu;
use cuzk_vk::msm::MegaMsmDenseStrip;

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

fn vulkan_device_for_smoke() -> Option<VulkanDevice> {
    if skip_vulkan_smoke() {
        return None;
    }
    match VulkanDevice::new() {
        Ok(d) => Some(d),
        Err(e) => {
            eprintln!(
                "skip: Vulkan init failed with CUZK_VK_SKIP_SMOKE=0 ({e:#})\n\
                 hint (macOS): install loader + MoltenVK (e.g. brew) and use extern/cuzk/apple-m2-vulkan-smoke.sh, or set DYLD_FALLBACK_LIBRARY_PATH to find libvulkan.dylib."
            );
            None
        }
    }
}

fn oracle_pair(strip: MegaMsmDenseStrip, tid: u32) -> (u32, u32) {
    let gxc = strip.groups_x_per_circuit as u64;
    let lx = strip.local_x as u64;
    let mut strip_sz = gxc.saturating_mul(lx);
    if strip_sz == 0 {
        strip_sz = 1;
    }
    let batch = strip.batch_circuits.max(1) as u64;
    let circ = (tid as u64 / strip_sz).min(batch.saturating_sub(1)) as u32;
    let in_strip = (tid as u64 - circ as u64 * strip_sz) as u32;
    (circ, in_strip)
}

#[test]
fn mega_strip_circuit_hit_matches_oracle() {
    let Some(dev) = vulkan_device_for_smoke() else {
        return;
    };
    let strips = [
        MegaMsmDenseStrip {
            groups_x_per_circuit: 2,
            batch_circuits: 3,
            local_x: 64,
        },
        MegaMsmDenseStrip {
            groups_x_per_circuit: 5,
            batch_circuits: 2,
            local_x: 32,
        },
    ];
    for strip in strips {
        let got = run_g1_mega_strip_circuit_hit_gpu(&dev, strip).expect("mega strip gpu");
        let total = got.len();
        for (tid, &pair) in got.iter().enumerate() {
            let want = oracle_pair(strip, tid as u32);
            assert_eq!(pair, want, "strip={strip:?} tid={tid} total={total}");
        }
    }
}
