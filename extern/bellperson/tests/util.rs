#[cfg(feature = "cuda-supraseal")]
pub mod supraseal {
    use std::io::Write;

    use bellperson::groth16::{Parameters, SuprasealParameters};
    use blstrs::Bls12;
    use tempfile::NamedTempFile;

    /// Returns the parameters in the way SupraSeal is expecting them.
    pub fn supraseal_params(params: Parameters<Bls12>) -> SuprasealParameters<Bls12> {
        // Write out parameters to a temp file
        let mut params_file = NamedTempFile::new().expect("failed to create temp parameters file");
        params
            .write(&mut params_file)
            .expect("failed to write out srs");
        params_file.flush().expect("failed to flush srs write");
        SuprasealParameters::new(params_file.path().to_path_buf()).expect("failed to read srs")
    }
}
