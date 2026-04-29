//! §C.1: save `VkPipelineCache` bytes and reload via `CUZK_VK_PIPELINE_CACHE` (serialized test — env mutex).

use std::sync::Mutex;

use cuzk_vk::device::VulkanDevice;
use cuzk_vk::toy_ntt::ntt_forward_8;
use cuzk_vk::toy_ntt_gpu::run_toy_ntt8_gpu;

static PIPELINE_CACHE_DISK_TEST_LOCK: Mutex<()> = Mutex::new(());

#[test]
fn pipeline_cache_save_load_roundtrip() {
    let _guard = PIPELINE_CACHE_DISK_TEST_LOCK.lock().expect("lock");

    let dev = match VulkanDevice::new() {
        Ok(d) => d,
        Err(_) => return,
    };

    let mut data = [1u32, 2, 3, 4, 5, 6, 7, 8];
    run_toy_ntt8_gpu(&dev, &mut data).expect("toy NTT");

    let path = std::env::temp_dir().join(format!(
        "cuzk_vk_pipeline_cache_{}.bin",
        std::process::id()
    ));
    let _ = std::fs::remove_file(&path);
    dev.pipeline_cache_save_to_path(&path).expect("save cache");

    let prev = std::env::var("CUZK_VK_PIPELINE_CACHE").ok();
    std::env::set_var("CUZK_VK_PIPELINE_CACHE", &path);

    let dev2 = match VulkanDevice::new() {
        Ok(d) => d,
        Err(e) => {
            if let Some(p) = prev {
                std::env::set_var("CUZK_VK_PIPELINE_CACHE", p);
            } else {
                std::env::remove_var("CUZK_VK_PIPELINE_CACHE");
            }
            let _ = std::fs::remove_file(&path);
            panic!("second VulkanDevice::new: {e:?}");
        }
    };

    let mut gpu = [1u32, 2, 3, 4, 5, 6, 7, 8];
    run_toy_ntt8_gpu(&dev2, &mut gpu).expect("toy NTT after reload");
    let mut want = [1u32, 2, 3, 4, 5, 6, 7, 8];
    ntt_forward_8(&mut want);

    if let Some(p) = prev {
        std::env::set_var("CUZK_VK_PIPELINE_CACHE", p);
    } else {
        std::env::remove_var("CUZK_VK_PIPELINE_CACHE");
    }
    let _ = std::fs::remove_file(&path);

    assert_eq!(gpu, want);
}
