# Commit 2

The final step of the sealing process is to generate a zkSNARK for the proof of replication (porep). Using the inclusion proofs from C1, the inputs are put through the porep circuit and a proof generated using Groth16.

## Intended Usage

The SupraSeal C2 operations are different than the rest of the library in that there are dependencies on primitives in external libraries. Specifically with bellperson through the use of a modified version of synthesize_circuits_batch() to generate the witness. From there the vectors are put through various MSM and NTT kernels on GPU and CPU. Note this requires the usage of a Rust based interface as opposed to the C/C++ seen throughout SupraSeal.

bellperson v0.26 interfaces to this implementation through `cuda-supraseal` feature.

To perform a 32GiB test/benchmark change directory to `demos/c2-test` and execute `cargo test --release -- --nocapture`. It's assumed that you've previously fetched the corresponding parameters. The expected execution time for the test is approximately 2-3 minutes depending on system.
