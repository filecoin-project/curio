//! CUDA pinned memory pool for fast GPU H2D transfers.
//!
//! ## Problem
//!
//! The NTT H2D transfer in `gpu_prove_start` copies a/b/c vectors from host
//! to device via `cudaMemcpyAsync`. When the source is regular heap memory,
//! CUDA stages through a small internal pinned bounce buffer, achieving only
//! 1-4 GB/s on PCIe Gen5. With pinned source memory, transfers run at full
//! PCIe bandwidth (~50 GB/s), reducing transfer time from seconds to ~40ms.
//!
//! ## Design
//!
//! `PinnedPool` manages a set of `cudaHostAlloc`'d buffers that are reused
//! across partition proofs. Synthesis writes a/b/c vectors directly into
//! pinned buffers (via `ProvingAssignment::new_with_pinned`), eliminating
//! both reallocation copies during synthesis and the slow staged H2D transfer.
//!
//! The pool is budget-integrated: allocated buffers count against the
//! `MemoryBudget`. The evictor can shrink the pool when SRS/PCE needs
//! memory.
//!
//! ## Buffer Lifecycle
//!
//! 1. Synthesis starts → check out 3 buffers (a, b, c) from pool
//! 2. `ProvingAssignment::new_with_pinned` uses them as backing store
//! 3. Synthesis fills a/b/c via `push()` into pinned memory
//! 4. GPU `prove_start` → `execute_ntts_single` copies at ~50 GB/s
//! 5. After GPU kernels, `release_abc()` returns buffers to pool
//! 6. Pool reuses buffers for next partition

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;

use tracing::{info, warn};

use crate::memory::GIB;

// ─── CUDA FFI ──────────────────────────────────────────────────────────────

extern "C" {
    fn cudaHostAlloc(
        pHost: *mut *mut std::ffi::c_void,
        size: usize,
        flags: std::ffi::c_uint,
    ) -> std::ffi::c_int;

    fn cudaFreeHost(ptr: *mut std::ffi::c_void) -> std::ffi::c_int;
}

/// `cudaHostAllocPortable` — memory is pinned and usable from all CUDA contexts.
const CUDA_HOST_ALLOC_PORTABLE: std::ffi::c_uint = 1;

/// Allocate CUDA pinned host memory.
///
/// Returns a pointer to `size` bytes of page-locked memory, or `None` if
/// the allocation fails (e.g., OS refuses to pin that much).
fn cuda_host_alloc(size: usize) -> Option<*mut u8> {
    let mut ptr: *mut std::ffi::c_void = std::ptr::null_mut();
    let err = unsafe { cudaHostAlloc(&mut ptr, size, CUDA_HOST_ALLOC_PORTABLE) };
    if err != 0 {
        warn!(
            size_gib = size as f64 / GIB as f64,
            cuda_err = err,
            "cudaHostAlloc failed"
        );
        return None;
    }
    Some(ptr as *mut u8)
}

/// Free CUDA pinned host memory.
fn cuda_host_free(ptr: *mut u8) {
    let err = unsafe { cudaFreeHost(ptr as *mut std::ffi::c_void) };
    if err != 0 {
        warn!(cuda_err = err, "cudaFreeHost failed");
    }
}

// ─── PinnedBuffer ──────────────────────────────────────────────────────────

/// A single pinned memory buffer.
///
/// `Send` because the buffer is page-locked physical memory — accessible
/// from any thread and any CUDA context.
pub struct PinnedBuffer {
    ptr: *mut u8,
    size: usize,
}

unsafe impl Send for PinnedBuffer {}
unsafe impl Sync for PinnedBuffer {}

impl PinnedBuffer {
    /// Raw pointer to the pinned memory.
    pub fn as_ptr(&self) -> *mut u8 {
        self.ptr
    }

    /// Size in bytes.
    pub fn size(&self) -> usize {
        self.size
    }

    /// Reconstruct a `PinnedBuffer` from raw parts.
    ///
    /// Used by the pool return callback to re-wrap raw pointers that were
    /// extracted from `PinnedAbcBuffers` and passed through `PinnedBacking`.
    ///
    /// # Safety (logical)
    ///
    /// `ptr` must be a pointer previously obtained from `cudaHostAlloc`
    /// (via `PinnedBuffer::as_ptr()`) and `size` must match the original
    /// allocation size.
    pub fn from_raw(ptr: *mut u8, size: usize) -> Self {
        Self { ptr, size }
    }
}

// ─── PinnedPool ────────────────────────────────────────────────────────────

/// Pool of reusable CUDA pinned memory buffers.
///
/// Thread-safe. Buffers are checked out for synthesis, used during GPU
/// proving, then checked back in for reuse. The pool grows on demand
/// and can be shrunk by the evictor when memory is needed elsewhere.
///
/// NOT budget-integrated: pinned memory replaces heap a/b/c allocations
/// that are already accounted for in per-partition budget reservations.
/// The pool is naturally bounded by `synthesis_concurrency × 3 buffers`.
pub struct PinnedPool {
    /// Free buffers available for checkout, grouped by size.
    free: Mutex<Vec<PinnedBuffer>>,

    /// Total bytes of pinned memory managed by this pool (allocated, not just free).
    total_bytes: AtomicU64,

    /// Total number of buffers ever allocated (for stats).
    total_allocated: AtomicU64,
}

impl PinnedPool {
    /// Create a new empty pool.
    pub fn new() -> Self {
        Self {
            free: Mutex::new(Vec::new()),
            total_bytes: AtomicU64::new(0),
            total_allocated: AtomicU64::new(0),
        }
    }

    /// Check out a pinned buffer of at least `min_bytes`.
    ///
    /// First tries to find a free buffer of sufficient size.
    /// If none available, allocates a new one via `cudaHostAlloc`
    /// (acquiring budget first). Returns `None` if budget is full
    /// and allocation is not possible.
    pub fn checkout(&self, min_bytes: usize) -> Option<PinnedBuffer> {
        // Try to find a suitable free buffer
        {
            let mut free = self.free.lock().unwrap();
            // Find smallest buffer >= min_bytes for best fit
            let mut best_idx: Option<usize> = None;
            let mut best_size = usize::MAX;
            for (i, buf) in free.iter().enumerate() {
                if buf.size >= min_bytes && buf.size < best_size {
                    best_idx = Some(i);
                    best_size = buf.size;
                }
            }
            if let Some(idx) = best_idx {
                let buf = free.swap_remove(idx);
                info!(
                    size_gib = buf.size as f64 / GIB as f64,
                    free_remaining = free.len(),
                    "pinned pool: checkout (reuse)"
                );
                return Some(buf);
            }
        }

        // No suitable buffer — allocate a new one
        self.allocate(min_bytes)
    }

    /// Return a buffer to the pool for reuse.
    pub fn checkin(&self, buf: PinnedBuffer) {
        info!(
            size_gib = buf.size as f64 / GIB as f64,
            "pinned pool: checkin"
        );
        self.free.lock().unwrap().push(buf);
    }

    /// Allocate a new pinned buffer via `cudaHostAlloc`.
    ///
    /// No budget check: pinned memory replaces heap a/b/c allocations
    /// already accounted for in per-partition budget reservations.
    fn allocate(&self, size: usize) -> Option<PinnedBuffer> {
        let ptr = match cuda_host_alloc(size) {
            Some(ptr) => ptr,
            None => {
                warn!(
                    size_gib = size as f64 / GIB as f64,
                    "pinned pool: cudaHostAlloc failed"
                );
                return None;
            }
        };

        self.total_bytes.fetch_add(size as u64, Ordering::Relaxed);
        let count = self.total_allocated.fetch_add(1, Ordering::Relaxed) + 1;
        info!(
            size_gib = size as f64 / GIB as f64,
            total_buffers = count,
            total_gib = self.total_bytes.load(Ordering::Relaxed) as f64 / GIB as f64,
            "pinned pool: allocated new buffer"
        );

        Some(PinnedBuffer { ptr, size })
    }

    /// Shrink the pool by freeing up to `target_bytes` of pinned memory.
    ///
    /// Called by the evictor when SRS/PCE needs more memory.
    /// Only frees buffers that are currently in the free list (not checked out).
    /// Returns the number of bytes actually freed.
    pub fn shrink(&self, target_bytes: u64) -> u64 {
        let mut freed = 0u64;
        let mut free = self.free.lock().unwrap();

        // Free largest buffers first (most impact per operation)
        free.sort_by(|a, b| b.size.cmp(&a.size));

        while freed < target_bytes {
            if let Some(buf) = free.pop() {
                let buf_size = buf.size as u64;
                cuda_host_free(buf.ptr);
                self.total_bytes.fetch_sub(buf_size, Ordering::Relaxed);
                freed += buf_size;
                info!(
                    freed_gib = buf_size as f64 / GIB as f64,
                    total_freed_gib = freed as f64 / GIB as f64,
                    "pinned pool: freed buffer (evictor)"
                );
            } else {
                break; // no more free buffers
            }
        }
        freed
    }

    /// Number of buffers currently in the free list.
    pub fn free_count(&self) -> usize {
        self.free.lock().unwrap().len()
    }

    /// Total bytes of pinned memory managed by this pool.
    pub fn total_bytes(&self) -> u64 {
        self.total_bytes.load(Ordering::Relaxed)
    }
}

impl Drop for PinnedPool {
    fn drop(&mut self) {
        let free = self.free.get_mut().unwrap();
        for buf in free.drain(..) {
            cuda_host_free(buf.ptr);
        }
    }
}

// ─── Triple Buffer Checkout ────────────────────────────────────────────────

/// Three pinned buffers for a/b/c, checked out together.
///
/// This is the unit passed to `ProvingAssignment::new_with_pinned`.
/// The `return_fn` callback returns all three buffers to the pool.
pub struct PinnedAbcBuffers {
    pub a: PinnedBuffer,
    pub b: PinnedBuffer,
    pub c: PinnedBuffer,
}

impl PinnedAbcBuffers {
    /// Check out three pinned buffers for a/b/c.
    ///
    /// `element_capacity` is the number of `Scalar` elements (= num_constraints).
    /// Each buffer is `element_capacity * scalar_size` bytes.
    pub fn checkout(
        pool: &PinnedPool,
        element_capacity: usize,
        scalar_size: usize,
    ) -> Option<Self> {
        let buf_bytes = element_capacity * scalar_size;

        let a = pool.checkout(buf_bytes)?;
        let b = match pool.checkout(buf_bytes) {
            Some(b) => b,
            None => {
                pool.checkin(a);
                return None;
            }
        };
        let c = match pool.checkout(buf_bytes) {
            Some(c) => c,
            None => {
                pool.checkin(a);
                pool.checkin(b);
                return None;
            }
        };

        Some(PinnedAbcBuffers { a, b, c })
    }
}
