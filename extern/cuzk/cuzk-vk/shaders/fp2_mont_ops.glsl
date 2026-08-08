// Fp2 over BLS12-381 Fp (Montgomery on both components). Requires `fp_helpers.glsl` + `@@FP_T@@` params.
// Non-residue is `u^2 = -1` in standard BLS12-381 tower; mul: (a0+a1 u)(b0+b1 u) = (a0 b0 - a1 b1) + (a0 b1 + a1 b0) u.

void fp2_mul_m(@@FP_T@@ a0, @@FP_T@@ a1, @@FP_T@@ b0, @@FP_T@@ b1, out @@FP_T@@ r0, out @@FP_T@@ r1) {
    @@FP_T@@ t00 = fp_mul_mont_cios(a0, b0);
    @@FP_T@@ t11 = fp_mul_mont_cios(a1, b1);
    @@FP_T@@ t01 = fp_mul_mont_cios(a0, b1);
    @@FP_T@@ t10 = fp_mul_mont_cios(a1, b0);
    r0 = fp_sub_mod(t00, t11);
    r1 = fp_add_mod(t01, t10);
}

void fp2_add_c(@@FP_T@@ a0, @@FP_T@@ a1, @@FP_T@@ b0, @@FP_T@@ b1, out @@FP_T@@ r0, out @@FP_T@@ r1) {
    r0 = fp_add_mod(a0, b0);
    r1 = fp_add_mod(a1, b1);
}

void fp2_sub_c(@@FP_T@@ a0, @@FP_T@@ a1, @@FP_T@@ b0, @@FP_T@@ b1, out @@FP_T@@ r0, out @@FP_T@@ r1) {
    r0 = fp_sub_mod(a0, b0);
    r1 = fp_sub_mod(a1, b1);
}

void fp2_sqr_m(@@FP_T@@ a0, @@FP_T@@ a1, out @@FP_T@@ r0, out @@FP_T@@ r1) {
    @@FP_T@@ t0 = fp_mul_mont_cios(a0, a0);
    @@FP_T@@ t1 = fp_mul_mont_cios(a1, a1);
    @@FP_T@@ t01 = fp_mul_mont_cios(a0, a1);
    @@FP_T@@ two = fp_dbl(t01);
    r0 = fp_sub_mod(t0, t1);
    r1 = two;
}

void fp2_dbl_c(@@FP_T@@ a0, @@FP_T@@ a1, out @@FP_T@@ r0, out @@FP_T@@ r1) {
    r0 = fp_add_mod(a0, a0);
    r1 = fp_add_mod(a1, a1);
}

bool fp2_is_zero(@@FP_T@@ a0, @@FP_T@@ a1) {
    return fp_is_zero(a0) && fp_is_zero(a1);
}

bool fp2_eq(@@FP_T@@ a0, @@FP_T@@ a1, @@FP_T@@ b0, @@FP_T@@ b1) {
    return fp_eq(a0, b0) && fp_eq(a1, b1);
}
