use crate::LinearCombination;
use ec_gpu_gen::multiexp_cpu::DensityTracker;
use ff::PrimeField;

/// Prefetch a cache line into L1 data cache.
///
/// On x86_64 this emits a PREFETCHT0 instruction. On other architectures
/// it is a no-op (the compiler may still emit a HW prefetch hint).
#[inline(always)]
fn prefetch_read<T>(ptr: *const T) {
    #[cfg(target_arch = "x86_64")]
    unsafe {
        core::arch::x86_64::_mm_prefetch(ptr as *const i8, core::arch::x86_64::_MM_HINT_T0);
    }
    #[cfg(not(target_arch = "x86_64"))]
    {
        let _ = ptr;
    }
}

#[inline(always)]
pub fn eval_with_trackers<Scalar: PrimeField>(
    lc: &LinearCombination<Scalar>,
    mut input_density: Option<&mut DensityTracker>,
    mut aux_density: Option<&mut DensityTracker>,
    input_assignment: &[Scalar],
    aux_assignment: &[Scalar],
) -> Scalar {
    let mut acc = Scalar::ZERO;
    let one = Scalar::ONE;

    let inp_terms = lc.input_terms_slice();
    for (i, (index, coeff)) in inp_terms.iter().enumerate() {
        if i + 1 < inp_terms.len() {
            prefetch_read(unsafe { input_assignment.as_ptr().add(inp_terms[i + 1].0) });
        }
        if !coeff.is_zero_vartime() {
            let mut tmp = input_assignment[*index];
            if coeff != &one {
                tmp *= coeff;
            }
            acc += tmp;

            if let Some(ref mut v) = input_density {
                v.inc(*index);
            }
        }
    }

    let aux_terms = lc.aux_terms_slice();
    for (i, (index, coeff)) in aux_terms.iter().enumerate() {
        if i + 1 < aux_terms.len() {
            prefetch_read(unsafe { aux_assignment.as_ptr().add(aux_terms[i + 1].0) });
        }
        if !coeff.is_zero_vartime() {
            let mut tmp = aux_assignment[*index];
            if coeff != &one {
                tmp *= coeff;
            }
            acc += tmp;

            if let Some(ref mut v) = aux_density {
                v.inc(*index);
            }
        }
    }

    acc
}

/// Evaluate A and B linear combinations simultaneously.
///
/// By interleaving the evaluation of two independent LCs that read from the
/// same assignment arrays, we give the CPU's out-of-order engine two
/// independent accumulator chains to work on. This improves ILP utilization
/// on wide superscalar cores (Zen4 can sustain 6+ IPC with enough independent
/// work in flight).
///
/// The density trackers for A and B are updated as in `eval_with_trackers`.
#[inline(always)]
pub fn eval_ab_interleaved<Scalar: PrimeField>(
    lc_a: &LinearCombination<Scalar>,
    lc_b: &LinearCombination<Scalar>,
    a_aux_density: &mut DensityTracker,
    b_input_density: &mut DensityTracker,
    b_aux_density: &mut DensityTracker,
    input_assignment: &[Scalar],
    aux_assignment: &[Scalar],
) -> (Scalar, Scalar) {
    let mut acc_a = Scalar::ZERO;
    let mut acc_b = Scalar::ZERO;
    let one = Scalar::ONE;

    // Phase 1: Process input terms of A and B interleaved.
    // A has no input density tracker (full density); B has b_input_density.
    let a_inp = lc_a.input_terms_slice();
    let b_inp = lc_b.input_terms_slice();
    let a_inp_len = a_inp.len();
    let b_inp_len = b_inp.len();
    let min_inp = a_inp_len.min(b_inp_len);

    // Process paired terms — one from A, one from B per iteration.
    for i in 0..min_inp {
        // Prefetch next terms for both A and B.
        if i + 1 < a_inp_len {
            prefetch_read(unsafe { input_assignment.as_ptr().add(a_inp[i + 1].0) });
        }
        if i + 1 < b_inp_len {
            prefetch_read(unsafe { input_assignment.as_ptr().add(b_inp[i + 1].0) });
        }

        // Evaluate A input term (no density tracker for inputs in A).
        let (a_idx, a_coeff) = &a_inp[i];
        if !a_coeff.is_zero_vartime() {
            let mut tmp = input_assignment[*a_idx];
            if a_coeff != &one {
                tmp *= a_coeff;
            }
            acc_a += tmp;
        }

        // Evaluate B input term (with density tracker).
        let (b_idx, b_coeff) = &b_inp[i];
        if !b_coeff.is_zero_vartime() {
            let mut tmp = input_assignment[*b_idx];
            if b_coeff != &one {
                tmp *= b_coeff;
            }
            acc_b += tmp;
            b_input_density.inc(*b_idx);
        }
    }

    // Process remaining A input terms.
    for i in min_inp..a_inp_len {
        if i + 1 < a_inp_len {
            prefetch_read(unsafe { input_assignment.as_ptr().add(a_inp[i + 1].0) });
        }
        let (idx, coeff) = &a_inp[i];
        if !coeff.is_zero_vartime() {
            let mut tmp = input_assignment[*idx];
            if coeff != &one {
                tmp *= coeff;
            }
            acc_a += tmp;
        }
    }

    // Process remaining B input terms.
    for i in min_inp..b_inp_len {
        if i + 1 < b_inp_len {
            prefetch_read(unsafe { input_assignment.as_ptr().add(b_inp[i + 1].0) });
        }
        let (idx, coeff) = &b_inp[i];
        if !coeff.is_zero_vartime() {
            let mut tmp = input_assignment[*idx];
            if coeff != &one {
                tmp *= coeff;
            }
            acc_b += tmp;
            b_input_density.inc(*idx);
        }
    }

    // Phase 2: Process aux terms of A and B interleaved.
    let a_aux = lc_a.aux_terms_slice();
    let b_aux = lc_b.aux_terms_slice();
    let a_aux_len = a_aux.len();
    let b_aux_len = b_aux.len();
    let min_aux = a_aux_len.min(b_aux_len);

    for i in 0..min_aux {
        if i + 1 < a_aux_len {
            prefetch_read(unsafe { aux_assignment.as_ptr().add(a_aux[i + 1].0) });
        }
        if i + 1 < b_aux_len {
            prefetch_read(unsafe { aux_assignment.as_ptr().add(b_aux[i + 1].0) });
        }

        let (a_idx, a_coeff) = &a_aux[i];
        if !a_coeff.is_zero_vartime() {
            let mut tmp = aux_assignment[*a_idx];
            if a_coeff != &one {
                tmp *= a_coeff;
            }
            acc_a += tmp;
            a_aux_density.inc(*a_idx);
        }

        let (b_idx, b_coeff) = &b_aux[i];
        if !b_coeff.is_zero_vartime() {
            let mut tmp = aux_assignment[*b_idx];
            if b_coeff != &one {
                tmp *= b_coeff;
            }
            acc_b += tmp;
            b_aux_density.inc(*b_idx);
        }
    }

    for i in min_aux..a_aux_len {
        if i + 1 < a_aux_len {
            prefetch_read(unsafe { aux_assignment.as_ptr().add(a_aux[i + 1].0) });
        }
        let (idx, coeff) = &a_aux[i];
        if !coeff.is_zero_vartime() {
            let mut tmp = aux_assignment[*idx];
            if coeff != &one {
                tmp *= coeff;
            }
            acc_a += tmp;
            a_aux_density.inc(*idx);
        }
    }

    for i in min_aux..b_aux_len {
        if i + 1 < b_aux_len {
            prefetch_read(unsafe { aux_assignment.as_ptr().add(b_aux[i + 1].0) });
        }
        let (idx, coeff) = &b_aux[i];
        if !coeff.is_zero_vartime() {
            let mut tmp = aux_assignment[*idx];
            if coeff != &one {
                tmp *= coeff;
            }
            acc_b += tmp;
            b_aux_density.inc(*idx);
        }
    }

    (acc_a, acc_b)
}
