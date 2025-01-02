// Copyright Supranational LLC

sppark::cuda_error!();

use std::path::PathBuf;

#[repr(C)]
pub struct SRS {
    ptr: *const core::ffi::c_void,
}

impl Default for SRS {
    fn default() -> Self {
        Self {
            ptr: core::ptr::null(),
        }
    }
}

impl SRS {
    pub fn try_new(srs_path: PathBuf, cache: bool) -> Result<Self, cuda::Error> {
        extern "C" {
            fn create_SRS(
                ret: &mut SRS,
                srs_path: *const std::os::raw::c_char,
                cache: bool,
            ) -> cuda::Error;
        }
        let c_srs_path = std::ffi::CString::new(srs_path.to_str().unwrap()).unwrap();

        let mut ret = SRS::default();
        let err = unsafe { create_SRS(&mut ret, c_srs_path.as_ptr(), cache) };
        if err.code != 0 {
            Err(err)
        } else {
            Ok(ret)
        }
    }

    pub fn evict(&self) {
        extern "C" {
            fn evict_SRS(by_ref: &SRS);
        }
        unsafe { evict_SRS(self) };
    }
}

impl Drop for SRS {
    fn drop(&mut self) {
        extern "C" {
            fn drop_SRS(by_ref: &SRS);
        }
        unsafe { drop_SRS(self) };
        self.ptr = core::ptr::null();
    }
}

impl Clone for SRS {
    fn clone(&self) -> Self {
        extern "C" {
            fn clone_SRS(by_ref: &SRS) -> SRS;
        }
        unsafe { clone_SRS(self) }
    }
}

unsafe impl Sync for SRS {}
unsafe impl Send for SRS {}

pub fn generate_groth16_proof<S, D, PR>(
    ntt_a_scalars: &[*const S],
    ntt_b_scalars: &[*const S],
    ntt_c_scalars: &[*const S],
    ntt_scalars_actual_size: usize,
    input_assignments: &[*const S],
    aux_assignments: &[*const S],
    input_assignments_size: usize,
    aux_assignments_size: usize,
    a_aux_density_bv: &[D],
    b_g1_input_density_bv: &[D],
    b_g1_aux_density_bv: &[D],
    a_aux_total_density: usize,
    b_g1_input_total_density: usize,
    b_g1_aux_total_density: usize,
    num_circuits: usize,
    r_s: &[S],
    s_s: &[S],
    proofs: &mut [PR],
    srs: &SRS,
) {
    assert_eq!(ntt_a_scalars.len(), num_circuits);
    assert_eq!(ntt_b_scalars.len(), num_circuits);
    assert_eq!(ntt_c_scalars.len(), num_circuits);
    assert_eq!(input_assignments.len(), num_circuits);
    assert_eq!(aux_assignments.len(), num_circuits);
    assert_eq!(r_s.len(), num_circuits);
    assert_eq!(s_s.len(), num_circuits);
    assert_eq!(proofs.len(), num_circuits);

    let bv_element_size: usize = std::mem::size_of::<D>() * 8; // length of D in bits
    assert!(
        bv_element_size == 64,
        "only 64-bit elements in bit vectors are supported"
    );

    assert!(a_aux_density_bv.len() * bv_element_size >= aux_assignments_size);
    assert!(b_g1_aux_density_bv.len() * bv_element_size >= aux_assignments_size);

    let provers: Vec<_> = (0..num_circuits)
        .map(|c| Assignment::<S> {
            // Density of queries
            a_aux_density: a_aux_density_bv.as_ptr() as *const _,
            a_aux_bit_len: aux_assignments_size,
            a_aux_popcount: a_aux_total_density,

            b_inp_density: b_g1_input_density_bv.as_ptr() as *const _,
            b_inp_bit_len: input_assignments_size,
            b_inp_popcount: b_g1_input_total_density,

            b_aux_density: b_g1_aux_density_bv.as_ptr() as *const _,
            b_aux_bit_len: aux_assignments_size,
            b_aux_popcount: b_g1_aux_total_density,

            // Evaluations of A, B, C polynomials
            a: ntt_a_scalars[c],
            b: ntt_b_scalars[c],
            c: ntt_c_scalars[c],
            abc_size: ntt_scalars_actual_size,

            // Assignments of variables
            inp_assignment_data: input_assignments[c],
            inp_assignment_size: input_assignments_size,

            aux_assignment_data: aux_assignments[c],
            aux_assignment_size: aux_assignments_size,
        })
        .collect();

    let err = unsafe {
        generate_groth16_proofs_c(
            provers.as_ptr() as *const _,
            num_circuits,
            r_s.as_ptr() as *const _,
            s_s.as_ptr() as *const _,
            proofs.as_mut_ptr() as *mut _,
            srs,
        )
    };

    if err.code != 0 {
        panic!("{}", String::from(err));
    }
}

#[repr(C)]
pub struct Assignment<Scalar> {
    // Density of queries
    pub a_aux_density: *const usize,
    pub a_aux_bit_len: usize,
    pub a_aux_popcount: usize,

    pub b_inp_density: *const usize,
    pub b_inp_bit_len: usize,
    pub b_inp_popcount: usize,

    pub b_aux_density: *const usize,
    pub b_aux_bit_len: usize,
    pub b_aux_popcount: usize,

    // Evaluations of A, B, C polynomials
    pub a: *const Scalar,
    pub b: *const Scalar,
    pub c: *const Scalar,
    pub abc_size: usize,

    // Assignments of variables
    pub inp_assignment_data: *const Scalar,
    pub inp_assignment_size: usize,

    pub aux_assignment_data: *const Scalar,
    pub aux_assignment_size: usize,
}

extern "C" {
    fn generate_groth16_proofs_c(
        provers: *const core::ffi::c_void,
        num_circuits: usize,
        r_s: *const core::ffi::c_void,
        s_s: *const core::ffi::c_void,
        proofs: *mut core::ffi::c_void,
        srs: &SRS,
    ) -> cuda::Error;
}

pub fn generate_groth16_proofs<S, PR>(
    provers: &[Assignment<S>],
    r_s: &[S],
    s_s: &[S],
    proofs: &mut [PR],
    srs: &SRS,
) {
    let num_circuits = provers.len();

    assert_eq!(r_s.len(), num_circuits);
    assert_eq!(s_s.len(), num_circuits);
    assert_eq!(proofs.len(), num_circuits);

    let err = unsafe {
        generate_groth16_proofs_c(
            provers.as_ptr() as *const _,
            num_circuits,
            r_s.as_ptr() as *const _,
            s_s.as_ptr() as *const _,
            proofs.as_mut_ptr() as *mut _,
            srs,
        )
    };

    if err.code != 0 {
        panic!("{}", String::from(err));
    }
}
