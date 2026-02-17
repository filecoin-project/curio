//! The [Groth16] proving system.
//!
//! [Groth16]: https://eprint.iacr.org/2016/260

pub mod aggregate;
#[cfg(not(feature = "cuda-supraseal"))]
mod ext;
#[cfg(feature = "cuda-supraseal")]
mod ext_supraseal;
mod generator;
#[cfg(not(target_arch = "wasm32"))]
mod mapped_params;
mod params;
mod proof;
mod prover;
#[cfg(feature = "cuda-supraseal")]
mod supraseal_params;
mod verifier;
mod verifying_key;

mod multiscalar;

#[cfg(not(feature = "cuda-supraseal"))]
pub use self::ext::*;
#[cfg(feature = "cuda-supraseal")]
pub use self::ext_supraseal::*;
pub use self::generator::*;
#[cfg(not(target_arch = "wasm32"))]
pub use self::mapped_params::*;
pub use self::params::*;
pub use self::proof::*;
#[cfg(feature = "cuda-supraseal")]
pub use self::prover::supraseal::{prove_from_assignments, synthesize_circuits_batch};
#[cfg(feature = "cuda-supraseal")]
pub use self::prover::ProvingAssignment;
#[cfg(feature = "cuda-supraseal")]
pub use self::supraseal_params::SuprasealParameters;
pub use self::verifier::*;
pub use self::verifying_key::*;
