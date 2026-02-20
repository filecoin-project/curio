//! Prover implementation implemented using SupraSeal (C++).

use std::{
    sync::{Arc, Mutex},
    time::Instant,
};

use bellpepper_core::{Circuit, ConstraintSystem, Index, SynthesisError, Variable};
use ff::{Field, PrimeField};
use log::info;
use pairing::MultiMillerLoop;
use rayon::iter::{IntoParallelIterator, IntoParallelRefMutIterator, ParallelIterator};

use super::{ParameterSource, Proof, ProvingAssignment};
use crate::{gpu::GpuName, BELLMAN_VERSION};

#[allow(clippy::type_complexity)]
pub(super) fn create_proof_batch_priority_inner<E, C, P: ParameterSource<E>>(
    circuits: Vec<C>,
    params: P,
    randomization: Option<(Vec<E::Fr>, Vec<E::Fr>)>,
    _priority: bool,
) -> Result<Vec<Proof<E>>, SynthesisError>
where
    E: MultiMillerLoop,
    C: Circuit<E::Fr> + Send,
    E::Fr: GpuName,
    E::G1Affine: GpuName,
    E::G2Affine: GpuName,
{
    info!(
        "Bellperson {} with SupraSeal is being used!",
        BELLMAN_VERSION
    );

    let (start, provers, input_assignments_no_repr, aux_assignments_no_repr) =
        synthesize_circuits_batch(circuits)?;

    let input_assignment_len = input_assignments_no_repr[0].len();
    let aux_assignment_len = aux_assignments_no_repr[0].len();
    let n = provers[0].a.len();
    let a_aux_density_total = provers[0].a_aux_density.get_total_density();
    let b_input_density_total = provers[0].b_input_density.get_total_density();
    let b_aux_density_total = provers[0].b_aux_density.get_total_density();
    let num_circuits = provers.len();

    let (r_s, s_s) = randomization.unwrap_or((
        vec![E::Fr::ZERO; num_circuits],
        vec![E::Fr::ZERO; num_circuits],
    ));

    // Make sure all circuits have the same input len.
    for prover in &provers {
        assert_eq!(
            prover.a.len(),
            n,
            "only equaly sized circuits are supported"
        );
        debug_assert_eq!(
            a_aux_density_total,
            prover.a_aux_density.get_total_density(),
            "only identical circuits are supported"
        );
        debug_assert_eq!(
            b_input_density_total,
            prover.b_input_density.get_total_density(),
            "only identical circuits are supported"
        );
        debug_assert_eq!(
            b_aux_density_total,
            prover.b_aux_density.get_total_density(),
            "only identical circuits are supported"
        );
    }

    let mut input_assignments_ref = Vec::with_capacity(num_circuits);
    let mut aux_assignments_ref = Vec::with_capacity(num_circuits);
    for i in 0..num_circuits {
        input_assignments_ref.push(input_assignments_no_repr[i].as_ptr());
        aux_assignments_ref.push(aux_assignments_no_repr[i].as_ptr());
    }

    let mut a_ref = Vec::with_capacity(num_circuits);
    let mut b_ref = Vec::with_capacity(num_circuits);
    let mut c_ref = Vec::with_capacity(num_circuits);

    for prover in &provers {
        a_ref.push(prover.a.as_ptr());
        b_ref.push(prover.b.as_ptr());
        c_ref.push(prover.c.as_ptr());
    }

    let mut proofs: Vec<Proof<E>> = Vec::with_capacity(num_circuits);
    // We call out to C++ code which is unsafe anyway, hence silence this warning.
    #[allow(clippy::uninit_vec)]
    unsafe {
        proofs.set_len(num_circuits);
    }

    let srs = params.get_supraseal_srs().ok_or_else(|| {
        log::error!("SupraSeal SRS wasn't allocated correctly");
        SynthesisError::MalformedSrs
    })?;
    supraseal_c2::generate_groth16_proof(
        a_ref.as_slice(),
        b_ref.as_slice(),
        c_ref.as_slice(),
        provers[0].a.len(),
        input_assignments_ref.as_mut_slice(),
        aux_assignments_ref.as_mut_slice(),
        input_assignment_len,
        aux_assignment_len,
        provers[0].a_aux_density.bv.as_raw_slice(),
        provers[0].b_input_density.bv.as_raw_slice(),
        provers[0].b_aux_density.bv.as_raw_slice(),
        a_aux_density_total,
        b_input_density_total,
        b_aux_density_total,
        num_circuits,
        r_s.as_slice(),
        s_s.as_slice(),
        proofs.as_mut_slice(),
        srs,
        core::ptr::null_mut(), // no external mutex — uses C++ fallback
    );

    let proof_time = start.elapsed();
    info!("prover time: {:?}", proof_time);

    Ok(proofs)
}

// Phase 12: Pending proof handle — holds the C++ handle and all Rust-side
// data that must stay alive until finalization.
pub struct PendingProofHandle<E: MultiMillerLoop> {
    /// Opaque C++ pending proof handle
    handle: *mut std::ffi::c_void,
    /// Number of circuits (proofs) in this batch
    num_circuits: usize,
    /// Rust-side data kept alive until finalize (async dealloc after)
    provers: Vec<ProvingAssignment<E::Fr>>,
    input_assignments: Vec<Arc<Vec<E::Fr>>>,
    aux_assignments: Vec<Arc<Vec<E::Fr>>>,
    r_s: Vec<E::Fr>,
    s_s: Vec<E::Fr>,
}

// Safety: The C++ handle is thread-safe (it owns its data and the
// b_g2_msm thread). The Rust data is moved exclusively into the handle.
unsafe impl<E: MultiMillerLoop> Send for PendingProofHandle<E> {}

impl<E: MultiMillerLoop> Drop for PendingProofHandle<E> {
    fn drop(&mut self) {
        if !self.handle.is_null() {
            unsafe { supraseal_c2::drop_pending_proof(self.handle) };
            self.handle = std::ptr::null_mut();
        }
    }
}

/// Phase 12: Start GPU proving — returns after GPU lock release with
/// b_g2_msm still running in the background.
///
/// The GPU worker can immediately loop back for the next job.
/// Call `finish_pending_proof` to join b_g2_msm and get final proofs.
#[allow(clippy::type_complexity)]
pub fn prove_start<E, P: ParameterSource<E>>(
    provers: Vec<ProvingAssignment<E::Fr>>,
    input_assignments: Vec<Arc<Vec<E::Fr>>>,
    aux_assignments: Vec<Arc<Vec<E::Fr>>>,
    params: P,
    r_s: Vec<E::Fr>,
    s_s: Vec<E::Fr>,
    gpu_mtx: GpuMutexPtr,
) -> Result<PendingProofHandle<E>, SynthesisError>
where
    E: MultiMillerLoop,
    E::Fr: GpuName,
    E::G1Affine: GpuName,
    E::G2Affine: GpuName,
{
    let input_assignment_len = input_assignments[0].len();
    let aux_assignment_len = aux_assignments[0].len();
    let a_aux_density_total = provers[0].a_aux_density.get_total_density();
    let b_input_density_total = provers[0].b_input_density.get_total_density();
    let b_aux_density_total = provers[0].b_aux_density.get_total_density();
    let num_circuits = provers.len();

    let mut input_assignments_ref: Vec<_> = input_assignments.iter().map(|v| v.as_ptr()).collect();
    let mut aux_assignments_ref: Vec<_> = aux_assignments.iter().map(|v| v.as_ptr()).collect();
    let a_ref: Vec<_> = provers.iter().map(|p| p.a.as_ptr()).collect();
    let b_ref: Vec<_> = provers.iter().map(|p| p.b.as_ptr()).collect();
    let c_ref: Vec<_> = provers.iter().map(|p| p.c.as_ptr()).collect();

    let srs = params.get_supraseal_srs().ok_or_else(|| {
        log::error!("SupraSeal SRS wasn't allocated correctly");
        SynthesisError::MalformedSrs
    })?;

    let handle = supraseal_c2::start_groth16_proof(
        a_ref.as_slice(),
        b_ref.as_slice(),
        c_ref.as_slice(),
        provers[0].a.len(),
        input_assignments_ref.as_mut_slice(),
        aux_assignments_ref.as_mut_slice(),
        input_assignment_len,
        aux_assignment_len,
        provers[0].a_aux_density.bv.as_raw_slice(),
        provers[0].b_input_density.bv.as_raw_slice(),
        provers[0].b_aux_density.bv.as_raw_slice(),
        a_aux_density_total,
        b_input_density_total,
        b_aux_density_total,
        num_circuits,
        r_s.as_slice(),
        s_s.as_slice(),
        srs,
        gpu_mtx,
    );

    // The GPU kernels (NTT + MSM) are done and cudaHostUnregister has run.
    // The a/b/c evaluation vectors (~12 GiB per partition) are no longer
    // needed — only the density bitvecs and assignment data are still
    // referenced by the background prep_msm/b_g2_msm thread.
    // Free them now to reduce peak memory by ~12 GiB per pending partition.
    let mut provers = provers;
    for prover in &mut provers {
        prover.a = Vec::new();
        prover.b = Vec::new();
        prover.c = Vec::new();
    }

    // r_s/s_s are already copied into the C++ handle (pp->r_s_owned/s_s_owned).
    // Drop the Rust copies now — they're small but no reason to keep them.
    drop(r_s);
    drop(s_s);

    Ok(PendingProofHandle {
        handle,
        num_circuits,
        provers,
        input_assignments,
        aux_assignments,
        r_s: Vec::new(),
        s_s: Vec::new(),
    })
}

/// Phase 12: Finalize a pending proof — join b_g2_msm, run epilogue.
///
/// Returns the completed proofs. Consumes the pending handle.
pub fn finish_pending_proof<E>(
    mut pending: PendingProofHandle<E>,
) -> Result<Vec<Proof<E>>, SynthesisError>
where
    E: MultiMillerLoop,
    E::Fr: GpuName,
    E::G1Affine: GpuName,
    E::G2Affine: GpuName,
{
    let mut proofs: Vec<Proof<E>> = Vec::with_capacity(pending.num_circuits);
    #[allow(clippy::uninit_vec)]
    unsafe {
        proofs.set_len(pending.num_circuits);
    }

    supraseal_c2::finish_groth16_proof(pending.handle, proofs.as_mut_slice());

    // Mark handle as consumed so Drop doesn't call destroy_pending_proof
    pending.handle = std::ptr::null_mut();

    // Async dealloc of Rust-side synthesis data
    static DEALLOC_MTX: Mutex<()> = Mutex::new(());
    let provers = std::mem::take(&mut pending.provers);
    let input_assignments = std::mem::take(&mut pending.input_assignments);
    let aux_assignments = std::mem::take(&mut pending.aux_assignments);
    let r_s = std::mem::take(&mut pending.r_s);
    let s_s = std::mem::take(&mut pending.s_s);

    std::thread::spawn(move || {
        let _guard = DEALLOC_MTX.lock().unwrap();
        drop(provers);
        drop(input_assignments);
        drop(aux_assignments);
        drop(r_s);
        drop(s_s);
        log::debug!("BUFFERS[rust_dealloc_finish]: pending proof dealloc done");
    });

    Ok(proofs)
}

#[allow(clippy::type_complexity)]
pub fn synthesize_circuits_batch<Scalar, C>(
    circuits: Vec<C>,
) -> Result<
    (
        Instant,
        std::vec::Vec<ProvingAssignment<Scalar>>,
        std::vec::Vec<std::sync::Arc<std::vec::Vec<Scalar>>>,
        std::vec::Vec<std::sync::Arc<std::vec::Vec<Scalar>>>,
    ),
    SynthesisError,
>
where
    Scalar: PrimeField,
    C: Circuit<Scalar> + Send,
{
    synthesize_circuits_batch_with_hint(circuits, None)
}

/// Like `synthesize_circuits_batch`, but with optional pre-sizing hints.
#[allow(clippy::type_complexity)]
pub fn synthesize_circuits_batch_with_hint<Scalar, C>(
    circuits: Vec<C>,
    capacity_hint: Option<SynthesisCapacityHint>,
) -> Result<
    (
        Instant,
        std::vec::Vec<ProvingAssignment<Scalar>>,
        std::vec::Vec<std::sync::Arc<std::vec::Vec<Scalar>>>,
        std::vec::Vec<std::sync::Arc<std::vec::Vec<Scalar>>>,
    ),
    SynthesisError,
>
where
    Scalar: PrimeField,
    C: Circuit<Scalar> + Send,
{
    let start = Instant::now();

    let mut provers = circuits
        .into_par_iter()
        .map(|circuit| -> Result<_, SynthesisError> {
            let mut prover = if let Some(hint) = capacity_hint {
                ProvingAssignment::new_with_capacity(
                    hint.num_constraints,
                    hint.num_aux,
                    hint.num_inputs,
                )
            } else {
                ProvingAssignment::new()
            };

            prover.alloc_input(|| "", || Ok(Scalar::ONE))?;

            circuit.synthesize(&mut prover)?;

            for i in 0..prover.input_assignment.len() {
                prover.enforce(|| "", |lc| lc + Variable(Index::Input(i)), |lc| lc, |lc| lc);
            }

            Ok(prover)
        })
        .collect::<Result<Vec<_>, _>>()?;

    info!("synthesis time: {:?}", start.elapsed());

    // Start fft/multiexp prover timer
    let start = Instant::now();
    info!("starting proof timer");

    let input_assignments_no_repr = provers
        .par_iter_mut()
        .map(|prover| {
            let input_assignment = std::mem::take(&mut prover.input_assignment);
            Arc::new(input_assignment)
        })
        .collect::<Vec<_>>();

    let aux_assignments_no_repr = provers
        .par_iter_mut()
        .map(|prover| {
            let aux_assignment = std::mem::take(&mut prover.aux_assignment);
            Arc::new(aux_assignment)
        })
        .collect::<Vec<_>>();

    Ok((
        start,
        provers,
        input_assignments_no_repr,
        aux_assignments_no_repr,
    ))
}

/// Prove from pre-synthesized assignments using SupraSeal GPU.
///
/// This is the GPU-only phase of proving: takes the output of
/// `synthesize_circuits_batch()` and runs NTT + MSM on the GPU via
/// the SupraSeal C++ backend.
///
/// # Arguments
/// * `provers` — ProvingAssignment instances containing a/b/c evaluations and density trackers.
///   The `input_assignment` and `aux_assignment` fields must have been moved out
///   (via `std::mem::take`) before calling this function.
/// * `input_assignments` — Input witness vectors (extracted from provers).
/// * `aux_assignments` — Aux witness vectors (extracted from provers).
/// * `params` — Parameter source providing the SupraSeal SRS.
/// * `r_s` — Randomization r values, one per circuit.
/// * `s_s` — Randomization s values, one per circuit.
///
/// # Panics
/// Panics if circuits have different constraint counts or density profiles.
/// Phase 8: Raw pointer to an opaque GPU mutex (`std::mutex` on the C++ side).
///
/// The engine creates one C++ `std::mutex` per GPU via `alloc_gpu_mutex()`,
/// then passes its address as a `*mut c_void` through FFI into the C++
/// `generate_groth16_proofs_c`, which casts it to `std::mutex*` and uses
/// it to serialize only the CUDA kernel region.
///
/// A null pointer is safe — the C++ side falls back to a function-local static mutex.
pub type GpuMutexPtr = *mut std::ffi::c_void;

/// Marker: GpuMutexPtr is `Send` because it points to a heap-allocated
/// C++ std::mutex that outlives all workers.
#[derive(Clone, Copy)]
pub struct SendableGpuMutex(pub GpuMutexPtr);
unsafe impl Send for SendableGpuMutex {}
unsafe impl Sync for SendableGpuMutex {}

/// Pre-sizing hints for circuit synthesis (Phase 5/6 PCE).
///
/// When available, these let `synthesize_circuits_batch_with_hint`
/// pre-allocate ProvingAssignment vectors to the exact size needed,
/// eliminating re-allocation during constraint generation.
#[derive(Debug, Clone, Copy)]
pub struct SynthesisCapacityHint {
    pub num_constraints: usize,
    pub num_aux: usize,
    pub num_inputs: usize,
}

/// Allocate a C++ `std::mutex` on the heap. Returns an opaque pointer
/// for passing as `gpu_mtx` to proving functions.
///
/// The caller must call `free_gpu_mutex` when done to avoid leaking.
pub fn alloc_gpu_mutex() -> GpuMutexPtr {
    supraseal_c2::alloc_gpu_mutex()
}

/// Free a C++ `std::mutex` previously created by `alloc_gpu_mutex`.
///
/// # Safety
/// `ptr` must be a pointer returned by `alloc_gpu_mutex` and must not
/// have been freed already.
pub unsafe fn free_gpu_mutex(ptr: GpuMutexPtr) {
    supraseal_c2::free_gpu_mutex(ptr);
}

#[allow(clippy::type_complexity)]
pub fn prove_from_assignments<E, P: ParameterSource<E>>(
    provers: Vec<ProvingAssignment<E::Fr>>,
    input_assignments: Vec<Arc<Vec<E::Fr>>>,
    aux_assignments: Vec<Arc<Vec<E::Fr>>>,
    params: P,
    r_s: Vec<E::Fr>,
    s_s: Vec<E::Fr>,
    gpu_mtx: GpuMutexPtr,
) -> Result<Vec<Proof<E>>, SynthesisError>
where
    E: MultiMillerLoop,
    E::Fr: GpuName,
    E::G1Affine: GpuName,
    E::G2Affine: GpuName,
{
    let start = Instant::now();

    let input_assignment_len = input_assignments[0].len();
    let aux_assignment_len = aux_assignments[0].len();
    let n = provers[0].a.len();
    let a_aux_density_total = provers[0].a_aux_density.get_total_density();
    let b_input_density_total = provers[0].b_input_density.get_total_density();
    let b_aux_density_total = provers[0].b_aux_density.get_total_density();
    let num_circuits = provers.len();

    // Validate uniformity.
    for prover in &provers {
        assert_eq!(
            prover.a.len(),
            n,
            "only equally sized circuits are supported"
        );
        debug_assert_eq!(
            a_aux_density_total,
            prover.a_aux_density.get_total_density(),
            "only identical circuits are supported"
        );
        debug_assert_eq!(
            b_input_density_total,
            prover.b_input_density.get_total_density(),
            "only identical circuits are supported"
        );
        debug_assert_eq!(
            b_aux_density_total,
            prover.b_aux_density.get_total_density(),
            "only identical circuits are supported"
        );
    }

    let mut input_assignments_ref = Vec::with_capacity(num_circuits);
    let mut aux_assignments_ref = Vec::with_capacity(num_circuits);
    for i in 0..num_circuits {
        input_assignments_ref.push(input_assignments[i].as_ptr());
        aux_assignments_ref.push(aux_assignments[i].as_ptr());
    }

    let mut a_ref = Vec::with_capacity(num_circuits);
    let mut b_ref = Vec::with_capacity(num_circuits);
    let mut c_ref = Vec::with_capacity(num_circuits);

    for prover in &provers {
        a_ref.push(prover.a.as_ptr());
        b_ref.push(prover.b.as_ptr());
        c_ref.push(prover.c.as_ptr());
    }

    let mut proofs: Vec<Proof<E>> = Vec::with_capacity(num_circuits);
    #[allow(clippy::uninit_vec)]
    unsafe {
        proofs.set_len(num_circuits);
    }

    let srs = params.get_supraseal_srs().ok_or_else(|| {
        log::error!("SupraSeal SRS wasn't allocated correctly");
        SynthesisError::MalformedSrs
    })?;
    supraseal_c2::generate_groth16_proof(
        a_ref.as_slice(),
        b_ref.as_slice(),
        c_ref.as_slice(),
        provers[0].a.len(),
        input_assignments_ref.as_mut_slice(),
        aux_assignments_ref.as_mut_slice(),
        input_assignment_len,
        aux_assignment_len,
        provers[0].a_aux_density.bv.as_raw_slice(),
        provers[0].b_input_density.bv.as_raw_slice(),
        provers[0].b_aux_density.bv.as_raw_slice(),
        a_aux_density_total,
        b_input_density_total,
        b_aux_density_total,
        num_circuits,
        r_s.as_slice(),
        s_s.as_slice(),
        proofs.as_mut_slice(),
        srs,
        gpu_mtx,
    );

    let proof_time = start.elapsed();
    info!("GPU prove time: {:?}", proof_time);

    // Move large synthesis data (~130 GB for 10 circuits of 32 GiB PoRep)
    // into a background thread for deallocation, so the caller gets results
    // immediately. Each ProvingAssignment holds a/b/c Vec<Scalar> of ~4.17 GB
    // each, plus aux_assignment (~0.74 GB). Dropping synchronously adds ~10s
    // of munmap() overhead on Zen4.
    //
    // Phase 11 Intervention 1: Serialize Rust-side dealloc threads to prevent
    // concurrent munmap() TLB shootdown storms. At most 1 Rust dealloc thread
    // active at a time. Waiting on the mutex just delays memory reclamation.
    static DEALLOC_MTX: Mutex<()> = Mutex::new(());

    std::thread::spawn(move || {
        let _guard = DEALLOC_MTX.lock().unwrap();
        drop(provers);
        drop(input_assignments);
        drop(aux_assignments);
        drop(r_s);
        drop(s_s);
    });

    Ok(proofs)
}
