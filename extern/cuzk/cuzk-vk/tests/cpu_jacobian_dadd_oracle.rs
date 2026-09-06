//! Pure-CPU emulation of `g1_jacobian_add108_tail.comp`'s dadd-style add to verify the math
//! matches `blst_p1_add_or_double` (i.e. `G1Projective + G1Projective`) regardless of GPU.
//!
//! This isolates the shader formula from any Vulkan/MoltenVK plumbing.

use blstrs::{Fp, G1Affine, G1Projective};
use ff::Field;
use group::Group;
use rand::SeedableRng;
use rand_chacha::ChaCha8Rng;

fn cpu_jac_add(a: &G1Projective, b: &G1Projective) -> G1Projective {
    // Mirror the GLSL shader (dadd / add-1998-cmo-2 style):
    //   z1z1 = az^2; z2z2 = bz^2
    //   u1 = ax*z2z2;   u2 = bx*z1z1
    //   s1 = ay*bz*z2z2; s2 = by*az*z1z1
    //   if u1==u2 && s1==s2 → double
    //   h = u2 - u1; hh = h^2; hhh = h*hh
    //   r = s2 - s1; v = u1*hh; r2 = r^2
    //   X3 = r2 - hhh - 2v
    //   Y3 = r*(v - X3) - s1*hhh
    //   Z3 = az*bz*h
    let ax = a.x();
    let ay = a.y();
    let az = a.z();
    let bx = b.x();
    let by = b.y();
    let bz = b.z();

    if bool::from(az.is_zero()) {
        return *b;
    }
    if bool::from(bz.is_zero()) {
        return *a;
    }

    let z1z1 = az.square();
    let z2z2 = bz.square();
    let u1 = ax * z2z2;
    let u2 = bx * z1z1;
    let s1 = ay * bz * z2z2;
    let s2 = by * az * z1z1;

    if u1 == u2 && s1 == s2 {
        // Doubling: dbl-2009-l (matches shader's jac_double).
        let aa = ax.square();
        let bb = ay.square();
        let cc = bb.square();
        let mut d = ax + bb;
        d = d.square();
        d = d - aa - cc;
        d = d.double();
        let e = aa.double() + aa;
        let f = e.square();
        let z3 = (ay * az).double();
        let x3 = f - d - d;
        let mut c8 = cc.double();
        c8 = c8.double();
        c8 = c8.double();
        let y3 = (d - x3) * e - c8;
        return G1Projective::from_raw_unchecked(x3, y3, z3);
    }

    let h = u2 - u1;
    let hh = h.square();
    let hhh = h * hh;
    let r = s2 - s1;
    let v = u1 * hh;
    let r2 = r.square();
    let twov = v.double();
    let x3 = r2 - hhh - twov;
    let y3 = r * (v - x3) - s1 * hhh;
    let z3 = az * bz * h;
    G1Projective::from_raw_unchecked(x3, y3, z3)
}

#[test]
fn cpu_dadd_oracle_matches_blstrs_add() {
    let mut rng = ChaCha8Rng::from_seed([0xC1u8; 32]);
    for i in 0..64 {
        let a = G1Projective::random(&mut rng);
        let b = G1Projective::random(&mut rng);
        let want = G1Affine::from(a + b);
        let got = G1Affine::from(cpu_jac_add(&a, &b));
        assert_eq!(got, want, "iter {i}");
    }
    // Doubling case.
    let a = G1Projective::random(&mut rng);
    let want = G1Affine::from(a + a);
    let got = G1Affine::from(cpu_jac_add(&a, &a));
    assert_eq!(got, want, "doubling");
}

/// EFD `add-2007-bl` (the original formula in the shader before the dadd switch). Same affine
/// result as `cpu_jac_add` but different Jacobian intermediates. Used to fingerprint which
/// formula the GPU is actually executing.
fn cpu_jac_add_2007_bl(a: &G1Projective, b: &G1Projective) -> G1Projective {
    let ax = a.x();
    let ay = a.y();
    let az = a.z();
    let bx = b.x();
    let by = b.y();
    let bz = b.z();
    if bool::from(az.is_zero()) {
        return *b;
    }
    if bool::from(bz.is_zero()) {
        return *a;
    }
    let z1z1 = az.square();
    let z2z2 = bz.square();
    let u1 = ax * z2z2;
    let u2 = bx * z1z1;
    let s1 = ay * bz * z2z2;
    let s2 = by * az * z1z1;
    let h = u2 - u1;
    let i_ = h.double().square();
    let j = h * i_;
    let r = (s2 - s1).double();
    let v = u1 * i_;
    let x3 = r.square() - j - v - v;
    let mut y3 = (v - x3) * r;
    let s1j2 = (s1 * j).double();
    y3 -= s1j2;
    let mut z3 = az + bz;
    z3 = z3.square();
    z3 = z3 - z1z1 - z2z2;
    z3 = z3 * h;
    G1Projective::from_raw_unchecked(x3, y3, z3)
}

#[test]
fn fingerprint_first_pair_against_known_formulas() {
    // The actual GPU smoke run reports `got.x = 0x10aa91e6...` and `got.y = 0x05952907...` for
    // the very first random pair. If neither dadd nor add-2007-bl produces that point, the
    // shader is doing something other than a (correctly-implemented) Jacobian add.
    let mut rng = ChaCha8Rng::from_seed([0xC1u8; 32]);
    let a = G1Projective::random(&mut rng);
    let b = G1Projective::random(&mut rng);

    let dadd_proj = cpu_jac_add(&a, &b);
    let bl_proj = cpu_jac_add_2007_bl(&a, &b);
    let want = G1Affine::from(a + b);

    fn fp_hex(f: &Fp) -> String {
        let bytes = f.to_bytes_le();
        let mut s = String::with_capacity(2 + bytes.len() * 2);
        s.push_str("0x");
        for b in bytes.iter().rev() {
            s.push_str(&format!("{b:02x}"));
        }
        s
    }

    eprintln!("=== blstrs a + b (canonical affine) ===");
    eprintln!("want.x = {}", fp_hex(&want.x()));
    eprintln!("want.y = {}", fp_hex(&want.y()));

    eprintln!("=== CPU dadd (matches blst_p1_add_or_double) ===");
    eprintln!("Jac.X = {}", fp_hex(&dadd_proj.x()));
    eprintln!("Jac.Y = {}", fp_hex(&dadd_proj.y()));
    eprintln!("Jac.Z = {}", fp_hex(&dadd_proj.z()));

    eprintln!("=== CPU add-2007-bl ===");
    eprintln!("Jac.X = {}", fp_hex(&bl_proj.x()));
    eprintln!("Jac.Y = {}", fp_hex(&bl_proj.y()));
    eprintln!("Jac.Z = {}", fp_hex(&bl_proj.z()));

    let dadd_aff = G1Affine::from(dadd_proj);
    let bl_aff = G1Affine::from(bl_proj);
    eprintln!("dadd  affine x = {}", fp_hex(&dadd_aff.x()));
    eprintln!("bl    affine x = {}", fp_hex(&bl_aff.x()));
    assert_eq!(dadd_aff, want);
    assert_eq!(bl_aff, want);
}

#[test]
fn debug_first_pair() {
    // Print the first failing pair so we can sanity-check by hand.
    let mut rng = ChaCha8Rng::from_seed([0xC1u8; 32]);
    let a = G1Projective::random(&mut rng);
    let b = G1Projective::random(&mut rng);

    let want_proj = a + b;
    let want = G1Affine::from(want_proj);
    let got = G1Affine::from(cpu_jac_add(&a, &b));

    fn fp_hex(f: &Fp) -> String {
        let bytes = f.to_bytes_le();
        let mut s = String::with_capacity(2 + bytes.len() * 2);
        s.push_str("0x");
        for b in bytes.iter().rev() {
            s.push_str(&format!("{b:02x}"));
        }
        s
    }

    eprintln!("a.x = {}", fp_hex(&a.x()));
    eprintln!("a.y = {}", fp_hex(&a.y()));
    eprintln!("a.z = {}", fp_hex(&a.z()));
    eprintln!("b.x = {}", fp_hex(&b.x()));
    eprintln!("b.y = {}", fp_hex(&b.y()));
    eprintln!("b.z = {}", fp_hex(&b.z()));
    eprintln!("want.x  = {}", fp_hex(&want.x()));
    eprintln!("want.y  = {}", fp_hex(&want.y()));
    eprintln!("got.x   = {}", fp_hex(&got.x()));
    eprintln!("got.y   = {}", fp_hex(&got.y()));
    eprintln!("want_proj.x = {}", fp_hex(&want_proj.x()));
    eprintln!("want_proj.y = {}", fp_hex(&want_proj.y()));
    eprintln!("want_proj.z = {}", fp_hex(&want_proj.z()));
    assert_eq!(got, want);
}
