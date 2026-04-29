//! Link-time and lightweight API smoke tests for supraseal-c2.
//! Full CUDA proving remains in `supra_seal/demos/c2-test`.

use std::path::PathBuf;

use supraseal_c2::SRS;

#[test]
fn c2_link_smoke_srs_default_round_trip() {
    let _s = SRS::default();
}

#[test]
fn srs_try_new_invalid_path_returns_err() {
    let err = SRS::try_new(
        PathBuf::from("/nonexistent/cuzk_supraseal_bad_params.params"),
        false,
    )
    .expect_err("missing SRS file should error");
    assert_ne!(err.code, 0, "expected non-zero CUDA error code: {:?}", err);
}
