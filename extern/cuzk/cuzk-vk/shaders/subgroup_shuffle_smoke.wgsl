// Minimal compute shader to validate §B.3 pipeline: WGSL-in → naga → SPIR-V → Vulkan.
// Every lane writes subgroupShuffle(42, 0): broadcast lane 0's value within the subgroup.
//
// Requires subgroup shuffle support on the ICD (MoltenVK may fail pipeline creation).

@group(0) @binding(0)
var<storage, read_write> buf: array<u32>;

@compute @workgroup_size(128)
fn main(@builtin(global_invocation_id) gid: vec3<u32>) {
    let i = gid.x;
    if i >= 128u {
        return;
    }
    let v = subgroupShuffle(42u, 0u);
    buf[i] = v;
}
