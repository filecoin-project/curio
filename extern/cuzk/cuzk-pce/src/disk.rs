//! PCE disk persistence: save and load `PreCompiledCircuit` to/from binary files.
//!
//! File format (version 2 — raw binary):
//!   - 32-byte header: magic (4B) + version (4B) + circuit dimensions (20B) + reserved (4B)
//!   - For each CSR matrix (A, B, C):
//!     - row_ptrs: u64 element count + raw bytes (Vec<u32>)
//!     - cols:     u64 element count + raw bytes (Vec<u32>)
//!     - vals:     u64 element count + raw bytes (Vec<Scalar>, 32B each)
//!   - Density bitmaps: 3 × (u64 element count + raw bytes) for Vec<u64> words
//!     + 3 × (usize bit_len, usize popcount) as u64 pairs
//!
//! Raw binary format achieves ~5 GB/s load speed (NVMe-limited) vs ~0.6 GB/s
//! with bincode, because bulk Vec data is written/read as contiguous byte slices
//! with zero per-element overhead.
//!
//! Files are written atomically (write to `.tmp`, then rename) to prevent
//! corruption from interrupted writes.

use std::fs;
use std::io::{BufReader, BufWriter, Read, Write};
use std::path::Path;
use std::time::Instant;

use anyhow::{bail, Context};
use ff::PrimeField;
use tracing::info;

use crate::csr::{CsrMatrix, PreCompiledCircuit};
use crate::density::PreComputedDensity;

/// Magic bytes identifying a PCE file.
const PCE_MAGIC: [u8; 4] = *b"PCE\x02";

/// Current file format version.
const PCE_VERSION: u32 = 2;

/// Total header size in bytes.
const HEADER_SIZE: usize = 32;

/// On-disk header for quick validation.
#[derive(Debug, Clone, Copy)]
struct PceFileHeader {
    magic: [u8; 4],
    version: u32,
    num_inputs: u32,
    num_aux: u32,
    num_constraints: u32,
    total_nnz: u64,
    _reserved: u32,
}

impl PceFileHeader {
    fn from_pce<Scalar: PrimeField>(pce: &PreCompiledCircuit<Scalar>) -> Self {
        Self {
            magic: PCE_MAGIC,
            version: PCE_VERSION,
            num_inputs: pce.num_inputs,
            num_aux: pce.num_aux,
            num_constraints: pce.num_constraints,
            total_nnz: pce.total_nnz() as u64,
            _reserved: 0,
        }
    }

    fn write_to(&self, w: &mut impl Write) -> anyhow::Result<()> {
        w.write_all(&self.magic)?;
        w.write_all(&self.version.to_le_bytes())?;
        w.write_all(&self.num_inputs.to_le_bytes())?;
        w.write_all(&self.num_aux.to_le_bytes())?;
        w.write_all(&self.num_constraints.to_le_bytes())?;
        w.write_all(&self.total_nnz.to_le_bytes())?;
        w.write_all(&self._reserved.to_le_bytes())?;
        Ok(())
    }

    fn read_from(r: &mut impl Read) -> anyhow::Result<Self> {
        let mut buf = [0u8; HEADER_SIZE];
        r.read_exact(&mut buf)
            .context("failed to read PCE file header")?;

        let magic = [buf[0], buf[1], buf[2], buf[3]];
        let version = u32::from_le_bytes([buf[4], buf[5], buf[6], buf[7]]);
        let num_inputs = u32::from_le_bytes([buf[8], buf[9], buf[10], buf[11]]);
        let num_aux = u32::from_le_bytes([buf[12], buf[13], buf[14], buf[15]]);
        let num_constraints = u32::from_le_bytes([buf[16], buf[17], buf[18], buf[19]]);
        let total_nnz = u64::from_le_bytes([
            buf[20], buf[21], buf[22], buf[23], buf[24], buf[25], buf[26], buf[27],
        ]);
        let _reserved = u32::from_le_bytes([buf[28], buf[29], buf[30], buf[31]]);

        Ok(Self {
            magic,
            version,
            num_inputs,
            num_aux,
            num_constraints,
            total_nnz,
            _reserved,
        })
    }

    fn validate(&self) -> anyhow::Result<()> {
        if self.magic != PCE_MAGIC {
            bail!(
                "invalid PCE file magic: expected {:?}, got {:?}",
                PCE_MAGIC,
                self.magic
            );
        }
        if self.version != PCE_VERSION {
            bail!(
                "incompatible PCE file version: expected {}, got {}",
                PCE_VERSION,
                self.version
            );
        }
        Ok(())
    }
}

// ─── Raw byte I/O helpers ───────────────────────────────────────────────────

/// Write a Vec<u32> as: u64 element count + raw bytes.
fn write_vec_u32(w: &mut impl Write, v: &[u32]) -> anyhow::Result<()> {
    let count = v.len() as u64;
    w.write_all(&count.to_le_bytes())?;
    // Safety: &[u32] → &[u8] is always valid on any platform
    let bytes = unsafe { std::slice::from_raw_parts(v.as_ptr() as *const u8, v.len() * 4) };
    w.write_all(bytes)?;
    Ok(())
}

/// Read a Vec<u32> from: u64 element count + raw bytes.
fn read_vec_u32(r: &mut impl Read) -> anyhow::Result<Vec<u32>> {
    let mut count_buf = [0u8; 8];
    r.read_exact(&mut count_buf)?;
    let count = u64::from_le_bytes(count_buf) as usize;

    let mut v: Vec<u32> = Vec::with_capacity(count);
    // Safety: reading raw bytes into uninitialized u32 buffer
    unsafe {
        v.set_len(count);
        let bytes = std::slice::from_raw_parts_mut(v.as_mut_ptr() as *mut u8, count * 4);
        r.read_exact(bytes)?;
    }
    Ok(v)
}

/// Write a Vec<u64> as: u64 element count + raw bytes.
fn write_vec_u64(w: &mut impl Write, v: &[u64]) -> anyhow::Result<()> {
    let count = v.len() as u64;
    w.write_all(&count.to_le_bytes())?;
    let bytes = unsafe { std::slice::from_raw_parts(v.as_ptr() as *const u8, v.len() * 8) };
    w.write_all(bytes)?;
    Ok(())
}

/// Read a Vec<u64> from: u64 element count + raw bytes.
fn read_vec_u64(r: &mut impl Read) -> anyhow::Result<Vec<u64>> {
    let mut count_buf = [0u8; 8];
    r.read_exact(&mut count_buf)?;
    let count = u64::from_le_bytes(count_buf) as usize;

    let mut v: Vec<u64> = Vec::with_capacity(count);
    unsafe {
        v.set_len(count);
        let bytes = std::slice::from_raw_parts_mut(v.as_mut_ptr() as *mut u8, count * 8);
        r.read_exact(bytes)?;
    }
    Ok(v)
}

/// Write a Vec<Scalar> as: u64 element count + raw bytes.
///
/// Safety: assumes Scalar has a fixed, stable memory layout of `scalar_size` bytes.
/// This is true for blstrs::Scalar (= blst_fr = [u64; 4] = 32 bytes).
fn write_vec_scalar<Scalar: PrimeField>(w: &mut impl Write, v: &[Scalar]) -> anyhow::Result<()> {
    let scalar_size = std::mem::size_of::<Scalar>();
    let count = v.len() as u64;
    w.write_all(&count.to_le_bytes())?;
    let bytes =
        unsafe { std::slice::from_raw_parts(v.as_ptr() as *const u8, v.len() * scalar_size) };
    w.write_all(bytes)?;
    Ok(())
}

/// Read a Vec<Scalar> from: u64 element count + raw bytes.
///
/// Safety: same assumption as write_vec_scalar.
fn read_vec_scalar<Scalar: PrimeField>(r: &mut impl Read) -> anyhow::Result<Vec<Scalar>> {
    let scalar_size = std::mem::size_of::<Scalar>();
    let mut count_buf = [0u8; 8];
    r.read_exact(&mut count_buf)?;
    let count = u64::from_le_bytes(count_buf) as usize;

    // Allocate with proper alignment
    let mut v: Vec<Scalar> = Vec::with_capacity(count);
    unsafe {
        v.set_len(count);
        let bytes = std::slice::from_raw_parts_mut(v.as_mut_ptr() as *mut u8, count * scalar_size);
        r.read_exact(bytes)?;
    }
    Ok(v)
}

/// Write a u64 pair (used for density bitmap metadata).
fn write_u64_pair(w: &mut impl Write, a: u64, b: u64) -> anyhow::Result<()> {
    w.write_all(&a.to_le_bytes())?;
    w.write_all(&b.to_le_bytes())?;
    Ok(())
}

/// Read a u64 pair.
fn read_u64_pair(r: &mut impl Read) -> anyhow::Result<(u64, u64)> {
    let mut buf = [0u8; 16];
    r.read_exact(&mut buf)?;
    let a = u64::from_le_bytes([
        buf[0], buf[1], buf[2], buf[3], buf[4], buf[5], buf[6], buf[7],
    ]);
    let b = u64::from_le_bytes([
        buf[8], buf[9], buf[10], buf[11], buf[12], buf[13], buf[14], buf[15],
    ]);
    Ok((a, b))
}

// ─── CSR matrix I/O ─────────────────────────────────────────────────────────

fn write_csr<Scalar: PrimeField>(
    w: &mut impl Write,
    csr: &CsrMatrix<Scalar>,
) -> anyhow::Result<()> {
    write_vec_u32(w, &csr.row_ptrs)?;
    write_vec_u32(w, &csr.cols)?;
    write_vec_scalar(w, &csr.vals)?;
    Ok(())
}

fn read_csr<Scalar: PrimeField>(r: &mut impl Read) -> anyhow::Result<CsrMatrix<Scalar>> {
    let row_ptrs = read_vec_u32(r)?;
    let cols = read_vec_u32(r)?;
    let vals = read_vec_scalar(r)?;
    Ok(CsrMatrix {
        row_ptrs,
        cols,
        vals,
    })
}

// ─── Density I/O ────────────────────────────────────────────────────────────

fn write_density(w: &mut impl Write, d: &PreComputedDensity) -> anyhow::Result<()> {
    // a_aux
    write_vec_u64(w, &d.a_aux_density_words)?;
    write_u64_pair(w, d.a_aux_bit_len as u64, d.a_aux_popcount as u64)?;
    // b_input
    write_vec_u64(w, &d.b_input_density_words)?;
    write_u64_pair(w, d.b_input_bit_len as u64, d.b_input_popcount as u64)?;
    // b_aux
    write_vec_u64(w, &d.b_aux_density_words)?;
    write_u64_pair(w, d.b_aux_bit_len as u64, d.b_aux_popcount as u64)?;
    Ok(())
}

fn read_density(r: &mut impl Read) -> anyhow::Result<PreComputedDensity> {
    let a_aux_density_words = read_vec_u64(r)?;
    let (a_aux_bit_len, a_aux_popcount) = read_u64_pair(r)?;

    let b_input_density_words = read_vec_u64(r)?;
    let (b_input_bit_len, b_input_popcount) = read_u64_pair(r)?;

    let b_aux_density_words = read_vec_u64(r)?;
    let (b_aux_bit_len, b_aux_popcount) = read_u64_pair(r)?;

    Ok(PreComputedDensity {
        a_aux_density_words,
        a_aux_bit_len: a_aux_bit_len as usize,
        a_aux_popcount: a_aux_popcount as usize,
        b_input_density_words,
        b_input_bit_len: b_input_bit_len as usize,
        b_input_popcount: b_input_popcount as usize,
        b_aux_density_words,
        b_aux_bit_len: b_aux_bit_len as usize,
        b_aux_popcount: b_aux_popcount as usize,
    })
}

// ─── Public API ─────────────────────────────────────────────────────────────

/// Save a `PreCompiledCircuit` to disk using raw binary format.
///
/// Writes atomically: data goes to `path.tmp` first, then is renamed to `path`.
/// Uses a 64 MiB write buffer for sequential throughput.
///
/// Format: 32-byte header + raw byte dumps of all vectors.
/// This is ~10x faster than bincode for 25+ GiB PCE files.
pub fn save_to_disk<Scalar: PrimeField>(
    pce: &PreCompiledCircuit<Scalar>,
    path: &Path,
) -> anyhow::Result<()> {
    let start = Instant::now();

    let tmp_path = path.with_extension("tmp");

    // Ensure parent directory exists
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("failed to create directory: {}", parent.display()))?;
    }

    let file = fs::File::create(&tmp_path)
        .with_context(|| format!("failed to create temp file: {}", tmp_path.display()))?;
    // 64 MiB buffer for sequential write throughput
    let mut writer = BufWriter::with_capacity(64 * 1024 * 1024, file);

    // Write header
    let header = PceFileHeader::from_pce(pce);
    header.write_to(&mut writer)?;

    // Write CSR matrices (A, B, C)
    write_csr(&mut writer, &pce.a)?;
    write_csr(&mut writer, &pce.b)?;
    write_csr(&mut writer, &pce.c)?;

    // Write density bitmaps
    write_density(&mut writer, &pce.density)?;

    writer.flush()?;
    // Ensure data is on disk before rename
    writer.into_inner()?.sync_all()?;

    // Atomic rename
    fs::rename(&tmp_path, path).with_context(|| {
        format!(
            "failed to rename {} -> {}",
            tmp_path.display(),
            path.display()
        )
    })?;

    let duration = start.elapsed();
    let file_size = fs::metadata(path).map(|m| m.len()).unwrap_or(0);

    info!(
        path = %path.display(),
        file_size_gib = format!("{:.1}", file_size as f64 / (1024.0 * 1024.0 * 1024.0)),
        duration_ms = duration.as_millis(),
        write_speed_gbs = format!("{:.1}", file_size as f64 / duration.as_secs_f64() / 1e9),
        "PCE saved to disk (raw format)"
    );

    Ok(())
}

/// Load a `PreCompiledCircuit` from disk using raw binary format.
///
/// Validates the file header before loading the full payload.
/// Uses a 64 MiB read buffer for sequential throughput.
///
/// ~10x faster than bincode: bulk Vec data is read as contiguous byte slices.
pub fn load_from_disk<Scalar: PrimeField>(
    path: &Path,
) -> anyhow::Result<PreCompiledCircuit<Scalar>> {
    let start = Instant::now();

    let file_size = fs::metadata(path)
        .with_context(|| format!("PCE file not found: {}", path.display()))?
        .len();

    info!(
        path = %path.display(),
        file_size_gib = format!("{:.1}", file_size as f64 / (1024.0 * 1024.0 * 1024.0)),
        "loading PCE from disk"
    );

    let file = fs::File::open(path)
        .with_context(|| format!("failed to open PCE file: {}", path.display()))?;
    // 64 MiB buffer for sequential read throughput
    let mut reader = BufReader::with_capacity(64 * 1024 * 1024, file);

    // Read and validate header
    let header = PceFileHeader::read_from(&mut reader)?;
    header.validate()?;

    info!(
        num_inputs = header.num_inputs,
        num_aux = header.num_aux,
        num_constraints = header.num_constraints,
        total_nnz = header.total_nnz,
        "PCE file header valid, loading raw data"
    );

    // Read CSR matrices
    let a = read_csr(&mut reader).context("failed to read A matrix")?;
    let b = read_csr(&mut reader).context("failed to read B matrix")?;
    let c = read_csr(&mut reader).context("failed to read C matrix")?;

    // Read density bitmaps
    let density = read_density(&mut reader).context("failed to read density")?;

    let pce = PreCompiledCircuit {
        num_inputs: header.num_inputs,
        num_aux: header.num_aux,
        num_constraints: header.num_constraints,
        a,
        b,
        c,
        density,
    };

    // Sanity check: total nnz should match header
    if pce.total_nnz() as u64 != header.total_nnz {
        bail!(
            "PCE total_nnz mismatch: header says {}, loaded data has {}",
            header.total_nnz,
            pce.total_nnz(),
        );
    }

    let duration = start.elapsed();
    info!(
        path = %path.display(),
        duration_ms = duration.as_millis(),
        read_speed_gbs = format!("{:.1}", file_size as f64 / duration.as_secs_f64() / 1e9),
        summary = %pce.summary(),
        "PCE loaded from disk (raw format)"
    );

    Ok(pce)
}

/// Default filename for a PCE file given a circuit identifier string.
///
/// Example: `pce_filename("porep-32g")` → `"pce-porep-32g.bin"`
pub fn pce_filename(circuit_name: &str) -> String {
    format!("pce-{}.bin", circuit_name)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_header_roundtrip() {
        let header = PceFileHeader {
            magic: PCE_MAGIC,
            version: PCE_VERSION,
            num_inputs: 328,
            num_aux: 130_169_893,
            num_constraints: 130_278_869,
            total_nnz: 722_388_891,
            _reserved: 0,
        };

        let mut buf = Vec::new();
        header.write_to(&mut buf).unwrap();
        assert_eq!(buf.len(), HEADER_SIZE);

        let mut cursor = std::io::Cursor::new(&buf);
        let loaded = PceFileHeader::read_from(&mut cursor).unwrap();
        loaded.validate().unwrap();

        assert_eq!(loaded.num_inputs, 328);
        assert_eq!(loaded.num_aux, 130_169_893);
        assert_eq!(loaded.num_constraints, 130_278_869);
        assert_eq!(loaded.total_nnz, 722_388_891);
    }

    #[test]
    fn test_bad_magic_rejected() {
        let mut buf = vec![0u8; HEADER_SIZE];
        buf[0..4].copy_from_slice(b"BAD\x00");
        let mut cursor = std::io::Cursor::new(&buf);
        let header = PceFileHeader::read_from(&mut cursor).unwrap();
        assert!(header.validate().is_err());
    }

    #[test]
    fn test_bad_version_rejected() {
        let header = PceFileHeader {
            magic: PCE_MAGIC,
            version: 99,
            num_inputs: 0,
            num_aux: 0,
            num_constraints: 0,
            total_nnz: 0,
            _reserved: 0,
        };
        let mut buf = Vec::new();
        header.write_to(&mut buf).unwrap();
        let mut cursor = std::io::Cursor::new(&buf);
        let loaded = PceFileHeader::read_from(&mut cursor).unwrap();
        assert!(loaded.validate().is_err());
    }

    #[test]
    fn test_vec_u32_roundtrip() {
        let v = vec![1u32, 2, 3, 0xDEADBEEF, 0];
        let mut buf = Vec::new();
        write_vec_u32(&mut buf, &v).unwrap();
        let mut cursor = std::io::Cursor::new(&buf);
        let loaded = read_vec_u32(&mut cursor).unwrap();
        assert_eq!(v, loaded);
    }

    #[test]
    fn test_vec_u64_roundtrip() {
        let v = vec![1u64, 0xCAFEBABE_DEADBEEF, 0];
        let mut buf = Vec::new();
        write_vec_u64(&mut buf, &v).unwrap();
        let mut cursor = std::io::Cursor::new(&buf);
        let loaded = read_vec_u64(&mut cursor).unwrap();
        assert_eq!(v, loaded);
    }
}
