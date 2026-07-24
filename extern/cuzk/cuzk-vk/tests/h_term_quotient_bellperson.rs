//! Phase J: `fr_quotient_scalars_from_abc` matches bellperson `EvaluationDomain` H-FFT path.

use bellperson::domain::EvaluationDomain;
use bellperson::SynthesisError;
use blstrs::Scalar;
use cuzk_vk::h_term::fr_quotient_scalars_from_abc;
use ec_gpu_gen::threadpool::Worker;
use ff::Field;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn bellperson_h_scalars(
    a: &[Scalar],
    b: &[Scalar],
    c: &[Scalar],
) -> Result<Vec<Scalar>, SynthesisError> {
    let worker = Worker::new();
    let mut da = EvaluationDomain::from_coeffs(a.to_vec())?;
    let mut db = EvaluationDomain::from_coeffs(b.to_vec())?;
    let mut dc = EvaluationDomain::from_coeffs(c.to_vec())?;

    EvaluationDomain::ifft_many(&mut [&mut da, &mut db, &mut dc], &worker, &mut None)?;
    EvaluationDomain::coset_fft_many(&mut [&mut da, &mut db, &mut dc], &worker, &mut None)?;

    da.mul_assign(&worker, &db);
    drop(db);
    da.sub_assign(&worker, &dc);
    drop(dc);

    da.divide_by_z_on_coset(&worker);
    da.icoset_fft(&worker, &mut None)?;
    let coeffs = da.into_coeffs();
    let n = coeffs.len();
    Ok(if n > 0 {
        coeffs[..n.saturating_sub(1)].to_vec()
    } else {
        vec![]
    })
}

#[test]
fn h_quotient_matches_bellperson_small_random() {
    let mut rng = ChaCha8Rng::from_seed([0x48u8; 32]);
    let len = 37usize;
    let a: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();
    let b: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();
    let c: Vec<Scalar> = (0..len).map(|_| Scalar::random(&mut rng)).collect();

    let got = fr_quotient_scalars_from_abc(&a, &b, &c).expect("cuzk");
    let want = bellperson_h_scalars(&a, &b, &c).expect("bellperson");
    assert_eq!(got, want);
}

#[test]
fn h_quotient_matches_bellperson_powers_pattern() {
    let len = 64usize;
    let a: Vec<Scalar> = (0..len).map(|i| Scalar::from(i as u64 + 1)).collect();
    let b: Vec<Scalar> = (0..len).map(|i| Scalar::from(i as u64 + 3)).collect();
    let c: Vec<Scalar> = (0..len).map(|i| Scalar::from(i as u64 + 7)).collect();
    let got = fr_quotient_scalars_from_abc(&a, &b, &c).expect("cuzk");
    let want = bellperson_h_scalars(&a, &b, &c).expect("bellperson");
    assert_eq!(got, want);
}
