//! §8.4 **D.1** mega-strip **circuit id** tagging (no curve math). Shader: `g1_mega_strip_circuit_hit_tail.comp`.

use std::io::Cursor;

use anyhow::{Context, Result};
use ash::util::read_spv;

use crate::device::VulkanDevice;
use crate::msm::MegaMsmDenseStrip;
use crate::vk_oneshot;

const HEADER_WORDS: usize = 64;
const MAX_THREADS: u32 = 4096;
const BUF_ALIGN: u64 = 4096;

fn put_u32(buf: &mut [u8], word: usize, v: u32) {
    let o = word * 4;
    buf[o..o + 4].copy_from_slice(&v.to_le_bytes());
}

fn get_u32(buf: &[u8], word: usize) -> u32 {
    let o = word * 4;
    u32::from_le_bytes(buf[o..o + 4].try_into().unwrap())
}

/// Run mega-strip hit map; returns `(circuit_id, tid_in_strip)` per global thread index.
pub fn run_g1_mega_strip_circuit_hit_gpu(
    dev: &VulkanDevice,
    strip: MegaMsmDenseStrip,
) -> Result<Vec<(u32, u32)>> {
    let groups_x_per_circuit = strip.groups_x_per_circuit;
    let local_x = strip.local_x;
    let batch = strip.batch_circuits.max(1);
    let total_u64 = strip.groups_x_total() as u64 * local_x as u64;
    let total = u32::try_from(total_u64).map_err(|_| anyhow::anyhow!("mega-strip thread count overflow"))?;
    anyhow::ensure!(total > 0 && total <= MAX_THREADS);

    let spirv = include_bytes!(concat!(env!("OUT_DIR"), "/g1_mega_strip_circuit_hit.spv"));
    let spirv_words =
        read_spv(&mut Cursor::new(spirv.as_slice())).context("read_spv g1_mega_strip_circuit_hit")?;

    let u32_len = HEADER_WORDS + (total as usize) * 2;
    let buf_len = u32_len * 4;
    let buffer_size = ((buf_len as u64 + BUF_ALIGN - 1) / BUF_ALIGN) * BUF_ALIGN;

    let mut wbytes = vec![0u8; buffer_size as usize];
    put_u32(&mut wbytes, 0, total);
    put_u32(&mut wbytes, 1, groups_x_per_circuit);
    put_u32(&mut wbytes, 2, local_x);
    put_u32(&mut wbytes, 3, batch);

    let gx = groups_x_per_circuit.saturating_mul(batch).max(1);
    let mut out = vec![0u8; buffer_size as usize];
    unsafe {
        vk_oneshot::run_compute_1x_storage_buffer(
            dev,
            &spirv_words,
            buffer_size,
            buffer_size,
            (gx, 1, 1),
            &wbytes,
            buf_len,
            &mut out,
            None,
        )?;
    }

    let mut v = Vec::with_capacity(total as usize);
    for tid in 0..total {
        let o = HEADER_WORDS + (tid as usize) * 2;
        v.push((get_u32(&out, o), get_u32(&out, o + 1)));
    }
    Ok(v)
}
