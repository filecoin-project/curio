//! Phase M / F: `FrNttPlan` boundary validation (cheap host-only).

use blstrs::Scalar;
use cuzk_vk::ntt::FrNttPlan;
use ff::PrimeField;

#[test]
fn fr_ntt_plan_rejects_log_zero() {
    assert!(FrNttPlan::try_new(0).is_err());
}

#[test]
fn fr_ntt_plan_rejects_log_above_two_adicity() {
    assert!(FrNttPlan::try_new(Scalar::S + 1).is_err());
}

#[test]
fn fr_ntt_plan_accepts_log_14() {
    let p = FrNttPlan::try_new(14).expect("log 14");
    assert_eq!(p.n, 16384);
}
