//! BLS12-381 **Groth16 proof** byte sizes for a future Vulkan-emitted proof + bellperson pairing bridge (**B₂**).
//!
//! Layout matches common compressed encodings (`blstrs` / `blst`). Pairing verification wiring is still
//! out of crate; this module is ABI-only.

use blstrs::{G1Affine, G2Affine};
use group::GroupEncoding;

/// Compressed G1 affine (BLS12-381), 48 bytes.
pub const G1_COMPRESSED_BYTES: usize = 48;
/// Compressed G2 affine (BLS12-381), 96 bytes.
pub const G2_COMPRESSED_BYTES: usize = 96;

/// `Proof { a: G1, b: G2, c: G1 }` in compressed form (Groth16 verify input wire format).
pub const GROTH16_PROOF_COMPRESSED_BYTES: usize =
    G1_COMPRESSED_BYTES + G2_COMPRESSED_BYTES + G1_COMPRESSED_BYTES;

/// Write `A || B || C` compressed (same order as bellperson `groth16::Proof::write`).
#[inline]
pub fn write_groth16_proof_compressed(
    a: &G1Affine,
    b: &G2Affine,
    c: &G1Affine,
    out: &mut [u8; GROTH16_PROOF_COMPRESSED_BYTES],
) {
    let ab = a.to_bytes();
    let bb = b.to_bytes();
    let cb = c.to_bytes();
    let ar = ab.as_ref();
    let br = bb.as_ref();
    let cr = cb.as_ref();
    debug_assert_eq!(ar.len(), G1_COMPRESSED_BYTES);
    debug_assert_eq!(br.len(), G2_COMPRESSED_BYTES);
    debug_assert_eq!(cr.len(), G1_COMPRESSED_BYTES);
    out[..G1_COMPRESSED_BYTES].copy_from_slice(ar);
    out[G1_COMPRESSED_BYTES..G1_COMPRESSED_BYTES + G2_COMPRESSED_BYTES].copy_from_slice(br);
    out[G1_COMPRESSED_BYTES + G2_COMPRESSED_BYTES..].copy_from_slice(cr);
}
