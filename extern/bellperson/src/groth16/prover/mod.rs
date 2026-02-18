#[cfg(not(feature = "cuda-supraseal"))]
mod native;
#[cfg(feature = "cuda-supraseal")]
pub mod supraseal;

use std::fmt;

use bellpepper_core::{
    Circuit, ConstraintSystem, Index, LinearCombination, SynthesisError, Variable,
};
use ec_gpu_gen::multiexp_cpu::DensityTracker;
use ff::{Field, PrimeField};
use pairing::MultiMillerLoop;
use rand_core::RngCore;

#[cfg(not(feature = "cuda-supraseal"))]
use self::native as prover;
#[cfg(feature = "cuda-supraseal")]
use self::supraseal as prover;
use super::{ParameterSource, Proof};
use crate::{gpu::GpuName, lc};

/// Pool of pre-allocated Vec buffers for recycling `LinearCombination` temporaries.
///
/// Each `enforce` call creates 3 `LinearCombination`s, each with 2 `Indexer`s
/// (inputs + aux), totaling 6 Vec allocations per constraint. With ~130M constraints,
/// this amounts to ~780M malloc/free calls — roughly 34% of synthesis time.
///
/// This pool recycles those buffers: after eval, the Vecs are cleared (retaining
/// their heap capacity) and returned to the pool for the next constraint.
struct LcVecPool<Scalar> {
    /// Recycled buffers for Indexer values. We need 6 per enforce call
    /// (3 LCs × 2 Indexers each: inputs + aux).
    bufs: Vec<Vec<(usize, Scalar)>>,
}

impl<Scalar> LcVecPool<Scalar> {
    fn new() -> Self {
        Self { bufs: Vec::new() }
    }

    /// Take a cleared buffer from the pool, or allocate a new one if empty.
    #[inline]
    fn take(&mut self) -> Vec<(usize, Scalar)> {
        self.bufs.pop().unwrap_or_default()
    }

    /// Return a cleared buffer to the pool.
    #[inline]
    fn give(&mut self, mut buf: Vec<(usize, Scalar)>) {
        buf.clear();
        self.bufs.push(buf);
    }
}

pub struct ProvingAssignment<Scalar: PrimeField> {
    // Density of queries
    pub a_aux_density: DensityTracker,
    pub b_input_density: DensityTracker,
    pub b_aux_density: DensityTracker,

    // Evaluations of A, B, C polynomials
    pub a: Vec<Scalar>,
    pub b: Vec<Scalar>,
    pub c: Vec<Scalar>,

    // Assignments of variables
    pub input_assignment: Vec<Scalar>,
    pub aux_assignment: Vec<Scalar>,

    // Recycling pool for LC temporary buffers (avoids malloc/free per constraint)
    lc_pool: LcVecPool<Scalar>,
}

impl<Scalar: PrimeField> fmt::Debug for ProvingAssignment<Scalar> {
    fn fmt(&self, fmt: &mut fmt::Formatter) -> fmt::Result {
        fmt.debug_struct("ProvingAssignment")
            .field("a_aux_density", &self.a_aux_density)
            .field("b_input_density", &self.b_input_density)
            .field("b_aux_density", &self.b_aux_density)
            .field(
                "a",
                &self
                    .a
                    .iter()
                    .map(|v| format!("Fr({:?})", v))
                    .collect::<Vec<_>>(),
            )
            .field(
                "b",
                &self
                    .b
                    .iter()
                    .map(|v| format!("Fr({:?})", v))
                    .collect::<Vec<_>>(),
            )
            .field(
                "c",
                &self
                    .c
                    .iter()
                    .map(|v| format!("Fr({:?})", v))
                    .collect::<Vec<_>>(),
            )
            .field("input_assignment", &self.input_assignment)
            .field("aux_assignment", &self.aux_assignment)
            .field("lc_pool_bufs", &self.lc_pool.bufs.len())
            .finish()
    }
}

impl<Scalar: PrimeField> PartialEq for ProvingAssignment<Scalar> {
    fn eq(&self, other: &ProvingAssignment<Scalar>) -> bool {
        self.a_aux_density == other.a_aux_density
            && self.b_input_density == other.b_input_density
            && self.b_aux_density == other.b_aux_density
            && self.a == other.a
            && self.b == other.b
            && self.c == other.c
            && self.input_assignment == other.input_assignment
            && self.aux_assignment == other.aux_assignment
    }
}

impl<Scalar: PrimeField> ProvingAssignment<Scalar> {
    /// Construct a ProvingAssignment from pre-computed components (PCE path).
    ///
    /// Used by the Pre-Compiled Constraint Evaluator (Phase 5) to bypass
    /// circuit synthesis entirely. The a/b/c vectors and density trackers
    /// are computed externally via CSR sparse MatVec, and the witness
    /// assignments are produced by WitnessCS.
    ///
    /// The resulting ProvingAssignment is ready for `prove_from_assignments()`.
    pub fn from_pce(
        a: Vec<Scalar>,
        b: Vec<Scalar>,
        c: Vec<Scalar>,
        a_aux_density: DensityTracker,
        b_input_density: DensityTracker,
        b_aux_density: DensityTracker,
    ) -> Self {
        Self {
            a_aux_density,
            b_input_density,
            b_aux_density,
            a,
            b,
            c,
            input_assignment: Vec::new(), // extracted separately via std::mem::take pattern
            aux_assignment: Vec::new(),   // extracted separately
            lc_pool: LcVecPool::new(),
        }
    }

    /// Create a ProvingAssignment with pre-allocated vectors.
    ///
    /// The constraint count and aux variable count are deterministic for a
    /// given circuit topology (e.g., always ~130M for 32 GiB PoRep).
    /// Pre-sizing eliminates ~27 reallocation cycles per vector and avoids
    /// ~32 GiB of redundant memory copies during synthesis.
    pub fn new_with_capacity(num_constraints: usize, num_aux: usize, num_inputs: usize) -> Self {
        use bitvec::prelude::*;
        Self {
            a_aux_density: DensityTracker {
                bv: BitVec::<usize, Lsb0>::with_capacity(num_aux),
                total_density: 0,
            },
            b_input_density: DensityTracker {
                bv: BitVec::<usize, Lsb0>::with_capacity(num_inputs),
                total_density: 0,
            },
            b_aux_density: DensityTracker {
                bv: BitVec::<usize, Lsb0>::with_capacity(num_aux),
                total_density: 0,
            },
            a: Vec::with_capacity(num_constraints),
            b: Vec::with_capacity(num_constraints),
            c: Vec::with_capacity(num_constraints),
            input_assignment: Vec::with_capacity(num_inputs),
            aux_assignment: Vec::with_capacity(num_aux),
            lc_pool: LcVecPool::new(),
        }
    }
}

impl<Scalar: PrimeField> ConstraintSystem<Scalar> for ProvingAssignment<Scalar> {
    type Root = Self;

    fn new() -> Self {
        Self {
            a_aux_density: DensityTracker::new(),
            b_input_density: DensityTracker::new(),
            b_aux_density: DensityTracker::new(),
            a: vec![],
            b: vec![],
            c: vec![],
            input_assignment: vec![],
            aux_assignment: vec![],
            lc_pool: LcVecPool::new(),
        }
    }

    fn alloc<F, A, AR>(&mut self, _: A, f: F) -> Result<Variable, SynthesisError>
    where
        F: FnOnce() -> Result<Scalar, SynthesisError>,
        A: FnOnce() -> AR,
        AR: Into<String>,
    {
        self.aux_assignment.push(f()?);
        self.a_aux_density.add_element();
        self.b_aux_density.add_element();

        Ok(Variable(Index::Aux(self.aux_assignment.len() - 1)))
    }

    fn alloc_input<F, A, AR>(&mut self, _: A, f: F) -> Result<Variable, SynthesisError>
    where
        F: FnOnce() -> Result<Scalar, SynthesisError>,
        A: FnOnce() -> AR,
        AR: Into<String>,
    {
        self.input_assignment.push(f()?);
        self.b_input_density.add_element();

        Ok(Variable(Index::Input(self.input_assignment.len() - 1)))
    }

    fn enforce<A, AR, LA, LB, LC>(&mut self, _: A, a: LA, b: LB, c: LC)
    where
        A: FnOnce() -> AR,
        AR: Into<String>,
        LA: FnOnce(LinearCombination<Scalar>) -> LinearCombination<Scalar>,
        LB: FnOnce(LinearCombination<Scalar>) -> LinearCombination<Scalar>,
        LC: FnOnce(LinearCombination<Scalar>) -> LinearCombination<Scalar>,
    {
        // Take 6 recycled buffers from the pool (3 LCs × 2 Indexers each).
        // After eval, the cleared buffers go back to the pool.
        let a_inp = self.lc_pool.take();
        let a_aux = self.lc_pool.take();
        let b_inp = self.lc_pool.take();
        let b_aux = self.lc_pool.take();
        let c_inp = self.lc_pool.take();
        let c_aux = self.lc_pool.take();

        let a = a(LinearCombination::zero_recycled(a_inp, a_aux));
        let b = b(LinearCombination::zero_recycled(b_inp, b_aux));
        let c = c(LinearCombination::zero_recycled(c_inp, c_aux));

        let input_assignment = &self.input_assignment;
        let aux_assignment = &self.aux_assignment;

        let a_aux_density = &mut self.a_aux_density;
        let b_input_density = &mut self.b_input_density;
        let b_aux_density = &mut self.b_aux_density;

        let a_res = lc::eval_with_trackers(
            &a,
            None,
            Some(a_aux_density),
            input_assignment,
            aux_assignment,
        );

        let b_res = lc::eval_with_trackers(
            &b,
            Some(b_input_density),
            Some(b_aux_density),
            input_assignment,
            aux_assignment,
        );

        let c_res = c.eval(input_assignment, aux_assignment);

        self.a.push(a_res);
        self.b.push(b_res);
        self.c.push(c_res);

        // Recycle the LC buffers back into the pool.
        let (ai, aa) = a.recycle();
        let (bi, ba) = b.recycle();
        let (ci, ca) = c.recycle();
        self.lc_pool.give(ai);
        self.lc_pool.give(aa);
        self.lc_pool.give(bi);
        self.lc_pool.give(ba);
        self.lc_pool.give(ci);
        self.lc_pool.give(ca);
    }

    fn push_namespace<NR, N>(&mut self, _: N)
    where
        NR: Into<String>,
        N: FnOnce() -> NR,
    {
        // Do nothing; we don't care about namespaces in this context.
    }

    fn pop_namespace(&mut self) {
        // Do nothing; we don't care about namespaces in this context.
    }

    fn get_root(&mut self) -> &mut Self::Root {
        self
    }

    fn is_extensible() -> bool {
        true
    }

    fn extend(&mut self, other: &Self) {
        self.a_aux_density.extend(&other.a_aux_density, false);
        self.b_input_density.extend(&other.b_input_density, true);
        self.b_aux_density.extend(&other.b_aux_density, false);

        self.a.extend(&other.a);
        self.b.extend(&other.b);
        self.c.extend(&other.c);

        self.input_assignment
            // Skip first input, which must have been a temporarily allocated one variable.
            .extend(&other.input_assignment[1..]);
        self.aux_assignment.extend(&other.aux_assignment);
    }
}

pub(super) fn create_random_proof_batch_priority<E, C, R, P: ParameterSource<E>>(
    circuits: Vec<C>,
    params: P,
    rng: &mut R,
    priority: bool,
) -> Result<Vec<Proof<E>>, SynthesisError>
where
    E: MultiMillerLoop,
    C: Circuit<E::Fr> + Send,
    R: RngCore,
    E::Fr: GpuName,
    E::G1Affine: GpuName,
    E::G2Affine: GpuName,
{
    let r_s = (0..circuits.len())
        .map(|_| E::Fr::random(&mut *rng))
        .collect();
    let s_s = (0..circuits.len())
        .map(|_| E::Fr::random(&mut *rng))
        .collect();

    create_proof_batch_priority::<E, C, P>(circuits, params, r_s, s_s, priority)
}

/// creates a batch of proofs where the randomization vector is already
/// predefined
pub(super) fn create_proof_batch_priority<E, C, P: ParameterSource<E>>(
    circuits: Vec<C>,
    params: P,
    r_s: Vec<E::Fr>,
    s_s: Vec<E::Fr>,
    priority: bool,
) -> Result<Vec<Proof<E>>, SynthesisError>
where
    E: MultiMillerLoop,
    C: Circuit<E::Fr> + Send,
    E::Fr: GpuName,
    E::G1Affine: GpuName,
    E::G2Affine: GpuName,
{
    prover::create_proof_batch_priority_inner(circuits, params, Some((r_s, s_s)), priority)
}

#[cfg(test)]
mod tests {
    use super::*;

    use blstrs::Scalar as Fr;
    use rand::Rng;
    use rand_core::SeedableRng;
    use rand_xorshift::XorShiftRng;

    #[test]
    fn test_proving_assignment_extend() {
        let mut rng = XorShiftRng::from_seed([
            0x59, 0x62, 0xbe, 0x5d, 0x76, 0x3d, 0x31, 0x8d, 0x17, 0xdb, 0x37, 0x32, 0x54, 0x06,
            0xbc, 0xe5,
        ]);

        for k in &[2, 4, 8] {
            for j in &[10, 20, 50] {
                let count: usize = k * j;

                let mut full_assignment = ProvingAssignment::<Fr>::new();
                full_assignment
                    .alloc_input(|| "one", || Ok(<Fr as Field>::ONE))
                    .unwrap();

                let mut partial_assignments = Vec::with_capacity(count / k);
                for i in 0..count {
                    if i % k == 0 {
                        let mut p = ProvingAssignment::new();
                        p.alloc_input(|| "one", || Ok(<Fr as Field>::ONE)).unwrap();
                        partial_assignments.push(p)
                    }

                    let index: usize = i / k;
                    let partial_assignment = &mut partial_assignments[index];

                    if rng.gen() {
                        let el = Fr::random(&mut rng);
                        full_assignment
                            .alloc(|| format!("alloc:{},{}", i, k), || Ok(el))
                            .unwrap();
                        partial_assignment
                            .alloc(|| format!("alloc:{},{}", i, k), || Ok(el))
                            .unwrap();
                    }

                    if rng.gen() {
                        let el = Fr::random(&mut rng);
                        full_assignment
                            .alloc_input(|| format!("alloc_input:{},{}", i, k), || Ok(el))
                            .unwrap();
                        partial_assignment
                            .alloc_input(|| format!("alloc_input:{},{}", i, k), || Ok(el))
                            .unwrap();
                    }

                    // TODO: LinearCombination
                }

                let mut combined = ProvingAssignment::new();
                combined
                    .alloc_input(|| "one", || Ok(<Fr as Field>::ONE))
                    .unwrap();

                for assignment in partial_assignments.iter() {
                    combined.extend(assignment);
                }
                assert_eq!(combined, full_assignment);
            }
        }
    }
}
