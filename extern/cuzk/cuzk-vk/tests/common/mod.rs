//! Shared Groth16 **mul** circuit for integration tests (`tests/groth16_verify_tiny.rs`, etc.).

use bellperson::{Circuit, ConstraintSystem, SynthesisError};
use blstrs::Scalar;

/// Witness `a`, `b`; public input `c = a * b` (single `alloc_input` after implicit `ONE`).
#[derive(Clone)]
pub struct MulCircuit {
    pub a: Option<Scalar>,
    pub b: Option<Scalar>,
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
