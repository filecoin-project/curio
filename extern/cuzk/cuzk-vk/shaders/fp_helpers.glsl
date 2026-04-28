// Shared BLS12-381 Fp helpers (12 × u32 Montgomery / mod-p). Concat after `glsl_32_bit_field_params::<Fp>()`.
// Placeholders: @@FP_T@@, @@FP_P@@, @@FP_INV@@, @@FP_ONE@@

bool fp_is_zero(@@FP_T@@ a) {
    for (uint i = 0u; i < 12u; i++) {
        if (a.val[i] != 0u) {
            return false;
        }
    }
    return true;
}

bool fp_eq(@@FP_T@@ a, @@FP_T@@ b) {
    for (uint i = 0u; i < 12u; i++) {
        if (a.val[i] != b.val[i]) {
            return false;
        }
    }
    return true;
}

@@FP_T@@ fp_zero() {
    @@FP_T@@ o;
    for (uint i = 0u; i < 12u; i++) {
        o.val[i] = 0u;
    }
    return o;
}

@@FP_T@@ fp_one() {
    @@FP_T@@ o;
    for (uint i = 0u; i < 12u; i++) {
        o.val[i] = @@FP_ONE@@[i];
    }
    return o;
}

void mul_u32_pair(uint a, uint b, out uint hi, out uint lo) {
    const uint mask = 0xffffu;
    uint a0 = a & mask;
    uint a1 = a >> 16u;
    uint b0 = b & mask;
    uint b1 = b >> 16u;
    uint p0 = a0 * b0;
    uint p1 = a1 * b0;
    uint p2 = a0 * b1;
    uint p3 = a1 * b1;
    uint carry = (p0 >> 16u) + (p1 & mask) + (p2 & mask);
    lo = (p0 & mask) | ((carry & mask) << 16u);
    hi = (p1 >> 16u) + (p2 >> 16u) + p3 + (carry >> 16u);
}

struct U64 {
    uint lo;
    uint hi;
};

U64 u64_add_u32(U64 a, uint b) {
    uint nlo = a.lo + b;
    uint c0 = (nlo < a.lo) ? 1u : 0u;
    uint nhi = a.hi + c0;
    uint c1 = (nhi < a.hi) ? 1u : 0u;
    return U64(nlo, nhi + c1);
}

uint mac_with_carry_32(uint a, uint b, uint c, inout uint d) {
    uint mh;
    uint ml;
    mul_u32_pair(a, b, mh, ml);
    U64 pr = U64(ml, mh);
    pr = u64_add_u32(pr, c);
    pr = u64_add_u32(pr, d);
    d = pr.hi;
    return pr.lo;
}

bool fp_gte_val(@@FP_T@@ a, @@FP_T@@ b) {
    for (uint k = 0u; k < 12u; k++) {
        uint i = 11u - k;
        if (a.val[i] > b.val[i]) {
            return true;
        }
        if (a.val[i] < b.val[i]) {
            return false;
        }
    }
    return true;
}

@@FP_T@@ fp_load_p() {
    @@FP_T@@ p;
    for (uint i = 0u; i < 12u; i++) {
        p.val[i] = @@FP_P@@[i];
    }
    return p;
}

@@FP_T@@ fp_add_raw(@@FP_T@@ a, @@FP_T@@ b) {
    uint carry = 0u;
    for (uint i = 0u; i < 12u; i++) {
        uint old = a.val[i];
        a.val[i] = old + b.val[i] + carry;
        carry = (carry != 0u) ? ((old >= a.val[i]) ? 1u : 0u) : ((old > a.val[i]) ? 1u : 0u);
    }
    return a;
}

@@FP_T@@ fp_sub_raw(@@FP_T@@ a, @@FP_T@@ b) {
    uint borrow = 0u;
    for (uint i = 0u; i < 12u; i++) {
        uint old = a.val[i];
        a.val[i] = old - b.val[i] - borrow;
        borrow = (borrow != 0u) ? ((old <= a.val[i]) ? 1u : 0u) : ((old < a.val[i]) ? 1u : 0u);
    }
    return a;
}

@@FP_T@@ fp_add_mod(@@FP_T@@ a, @@FP_T@@ b) {
    @@FP_T@@ r = fp_add_raw(a, b);
    @@FP_T@@ p = fp_load_p();
    if (fp_gte_val(r, p)) {
        r = fp_sub_raw(r, p);
    }
    return r;
}

@@FP_T@@ fp_sub_mod(@@FP_T@@ a, @@FP_T@@ b) {
    @@FP_T@@ aa = a;
    @@FP_T@@ res = fp_sub_raw(aa, b);
    if (!fp_gte_val(a, b)) {
        @@FP_T@@ p = fp_load_p();
        res = fp_add_raw(res, p);
    }
    @@FP_T@@ p2 = fp_load_p();
    if (fp_gte_val(res, p2)) {
        res = fp_sub_raw(res, p2);
    }
    return res;
}

@@FP_T@@ fp_dbl(@@FP_T@@ a) {
    return fp_add_mod(a, a);
}

uint add_with_carry_32(uint a, inout uint b) {
    uint lo = a + b;
    b = (lo < a) ? 1u : 0u;
    return lo;
}

@@FP_T@@ fp_mul_mont_cios(@@FP_T@@ a, @@FP_T@@ b) {
    uint t[14];
    for (uint k = 0u; k < 14u; k++) {
        t[k] = 0u;
    }

    for (uint i = 0u; i < 12u; i++) {
        uint carry = 0u;
        for (uint j = 0u; j < 12u; j++) {
            t[j] = mac_with_carry_32(a.val[j], b.val[i], t[j], carry);
        }
        t[12] = add_with_carry_32(t[12], carry);
        t[13] = carry;

        carry = 0u;
        uint m = @@FP_INV@@ * t[0];
        t[0] = mac_with_carry_32(m, @@FP_P@@[0], t[0], carry);
        for (uint j = 1u; j < 12u; j++) {
            t[j - 1u] = mac_with_carry_32(m, @@FP_P@@[j], t[j], carry);
        }
        t[11] = add_with_carry_32(t[12], carry);
        t[12] = t[13] + carry;
    }

    @@FP_T@@ result;
    for (uint i = 0u; i < 12u; i++) {
        result.val[i] = t[i];
    }
    @@FP_T@@ p = fp_load_p();
    if (fp_gte_val(result, p)) {
        result = fp_sub_raw(result, p);
    }
    return result;
}
