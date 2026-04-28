//! Phase K: native Groth16 **prove + verify** on BLS12-381 (bellperson), independent of Vulkan.
//!
//! Establishes that the bellperson fork used by `cuzk-vk` tests is sound for a minimal R1CS; the
//! Vulkan backend (`prove_groth16_partition`) remains compute smoke until wired to this path.

use bellperson::groth16::{
    create_random_proof, generate_random_parameters, prepare_verifying_key, verify_proof,
};
use bellperson::{Circuit, ConstraintSystem, SynthesisError};
use blstrs::{Bls12, Scalar};
use ff::Field;
use rand::thread_rng;

/// Witness `a`, `b`; public input `c = a * b` (single `alloc_input` after implicit `ONE`).
#[derive(Clone)]
struct MulCircuit {
    a: Option<Scalar>,
    b: Option<Scalar>,
}

impl Circuit<Scalar> for MulCircuit {
    fn synthesize<CS: ConstraintSystem<Scalar>>(self, cs: &mut CS) -> Result<(), SynthesisError> {
        let a = cs.alloc(|| "a", || self.a.ok_or(SynthesisError::AssignmentMissing))?;
        let b = cs.alloc(|| "b", || self.b.ok_or(SynthesisError::AssignmentMissing))?;
        let c = cs.alloc_input(
            || "c",
            || {
                let aa = self.a.ok_or(SynthesisError::AssignmentMissing)?;
                let bb = self.b.ok_or(SynthesisError::AssignmentMissing)?;
                Ok(aa * bb)
            },
        )?;
        cs.enforce(|| "a*b=c", |lc| lc + a, |lc| lc + b, |lc| lc + c);
        Ok(())
    }
}

#[test]
fn groth16_mul_circuit_prove_and_verify() {
    let mut rng = thread_rng();
    let params = generate_random_parameters::<Bls12, _, _>(
        MulCircuit {
            a: Some(Scalar::ONE),
            b: Some(Scalar::ONE),
        },
        &mut rng,
    )
    .expect("paramgen");
    let pvk = prepare_verifying_key(&params.vk);

    let a = Scalar::from(7u64);
    let b = Scalar::from(11u64);
    let proof = create_random_proof(
        MulCircuit {
            a: Some(a),
            b: Some(b),
        },
        &params,
        &mut rng,
    )
    .expect("prove");

    assert!(
        verify_proof(&pvk, &proof, &[a * b]).expect("verify"),
        "Groth16 verify failed"
    );
}
