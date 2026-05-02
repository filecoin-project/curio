# Vulkan measurement matrix (§1)

Copy this block into commit messages or release notes when you land a perf batch. Fill every field once per **SKU + driver** row you care about.

## Row template

- **Date / commit:**  
- **GPU:** (marketing name)  
- **Driver / API:** (`driver_version`, `api_version` from partition CSV or `vulkaninfo`)  
- **Subgroup:** (`vulkaninfo` — wave size, `VK_KHR_subgroup_*` if relevant)  
- **Loader / ICD:** (e.g. MoltenVK + Homebrew loader path)  
- **Workload:** (32 GiB C2 `n_circuits`, WindowPoSt partition, or `prove_groth16_partition` smoke)  
- **End-to-end s/proof (if measured):**  
- **NTT s / MSM s split (if measured):**  
- **Peak VRAM (optional):**  
- **Artifacts:** path to `results-*.csv` and optional `CUZK_VK_BENCH_HARDWARE_MD` `.md` log  

## Reference

Repo protocol: [`README.md`](README.md) and root [`cuzk-vulkan-optimization-roadmap.md`](../../cuzk-vulkan-optimization-roadmap.md) §1 / §8.8.
