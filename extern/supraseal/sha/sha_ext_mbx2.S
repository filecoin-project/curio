// Copyright Supranational LLC

.intel_syntax noprefix

.global sha_ext_mbx2
.type sha_ext_mbx2, @function

.SECTION .text
  .ALIGN  32

.type   sha_ext_mbx2, @function
sha_ext_mbx2:
.cfi_startproc
  endbr64
  push    rbp
.cfi_adjust_cfa_offset  8
.cfi_offset             rbp,-16
  mov     rbp, rsp
.cfi_def_cfa_register   rbp
  push    r12
.cfi_offset             r12,-24
  push    r13
.cfi_offset             r13,-32
  sub     rsp, 64
  and     rsp, 0xFFFFFFFFFFFFFFF0
  movdqa  xmm1,  [FIRST_STATE+rip]
  movdqa  xmm2,  [SECOND_STATE+rip]
  movdqa  xmm8, xmm1
  movdqa  xmm9, xmm2
  lea     r10, [CONSTANTS+rip]
  mov     r13d, 1
  mov     r11, rsi
  mov     rsi,  [rsi]
  add     rsi, 192
  cmp     r9, 1
  jz      shaloop
  add     rsi, 64
shaloop:
  movdqa   [rsp], xmm1
  movdqa   [rsp+0x10], xmm2
  movdqa   [rsp+0x20], xmm8
  movdqa   [rsp+0x30], xmm9
  mov     r12,  [r11]
  add     r12, rcx
  movdqu  xmm0,  [r12]
  movdqa  xmm3, xmm0
  paddd   xmm0,  [r10]
  sha256rnds2 xmm2, xmm1, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  movdqu  xmm0,  [r12+0x20]
  movdqa  xmm10, xmm0
  paddd   xmm0,  [r10]
  sha256rnds2 xmm9, xmm8, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  movdqu  xmm0,  [r12+0x10]
  movdqa  xmm4, xmm0
  paddd   xmm0,  [r10+0x10]
  sha256rnds2 xmm2, xmm1, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm3, xmm4
  movdqu  xmm0,  [r12+0x30]
  movdqa  xmm11, xmm0
  paddd   xmm0,  [r10+0x10]
  sha256rnds2 xmm9, xmm8, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm10, xmm11
  mov     r12,  [r11+0x8]
  add     r12, rcx
  sub     r8, 1
  cmove   r12, rsi
  movdqu  xmm0,  [r12]
  movdqa  xmm5, xmm0
  paddd   xmm0,  [r10+0x20]
  sha256rnds2 xmm2, xmm1, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm4, xmm5
  movdqu  xmm0,  [r12+0x20]
  movdqa  xmm12, xmm0
  paddd   xmm0,  [r10+0x20]
  sha256rnds2 xmm9, xmm8, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm11, xmm12
  movdqu  xmm0,  [r12+0x10]
  movdqa  xmm6, xmm0
  paddd   xmm0,  [r10+0x30]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm6
  palignr xmm7, xmm5, 0x04
  paddd   xmm3, xmm7
  sha256msg2 xmm3, xmm6
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm5, xmm6
  movdqu  xmm0,  [r12+0x30]
  movdqa  xmm13, xmm0
  paddd   xmm0,  [r10+0x30]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm13
  palignr xmm14, xmm12, 0x04
  paddd   xmm10, xmm14
  sha256msg2 xmm10, xmm13
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm12, xmm13
  movdqa  xmm0, xmm3
  paddd   xmm0,  [r10+0x40]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm3
  palignr xmm7, xmm6, 0x04
  paddd   xmm4, xmm7
  sha256msg2 xmm4, xmm3
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm6, xmm3
  movdqa  xmm0, xmm10
  paddd   xmm0,  [r10+0x40]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm10
  palignr xmm14, xmm13, 0x04
  paddd   xmm11, xmm14
  sha256msg2 xmm11, xmm10
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm13, xmm10
  movdqa  xmm0, xmm4
  paddd   xmm0,  [r10+0x50]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm4
  palignr xmm7, xmm3, 0x04
  paddd   xmm5, xmm7
  sha256msg2 xmm5, xmm4
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm3, xmm4
  movdqa  xmm0, xmm11
  paddd   xmm0,  [r10+0x50]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm11
  palignr xmm14, xmm10, 0x04
  paddd   xmm12, xmm14
  sha256msg2 xmm12, xmm11
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm10, xmm11
  movdqa  xmm0, xmm5
  paddd   xmm0,  [r10+0x60]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm5
  palignr xmm7, xmm4, 0x04
  paddd   xmm6, xmm7
  sha256msg2 xmm6, xmm5
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm4, xmm5
  movdqa  xmm0, xmm12
  paddd   xmm0,  [r10+0x60]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm12
  palignr xmm14, xmm11, 0x04
  paddd   xmm13, xmm14
  sha256msg2 xmm13, xmm12
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm11, xmm12
  movdqa  xmm0, xmm6
  paddd   xmm0,  [r10+0x70]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm6
  palignr xmm7, xmm5, 0x04
  paddd   xmm3, xmm7
  sha256msg2 xmm3, xmm6
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm5, xmm6
  movdqa  xmm0, xmm13
  paddd   xmm0,  [r10+0x70]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm13
  palignr xmm14, xmm12, 0x04
  paddd   xmm10, xmm14
  sha256msg2 xmm10, xmm13
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm12, xmm13
  movdqa  xmm0, xmm3
  paddd   xmm0,  [r10+0x80]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm3
  palignr xmm7, xmm6, 0x04
  paddd   xmm4, xmm7
  sha256msg2 xmm4, xmm3
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm6, xmm3
  movdqa  xmm0, xmm10
  paddd   xmm0,  [r10+0x80]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm10
  palignr xmm14, xmm13, 0x04
  paddd   xmm11, xmm14
  sha256msg2 xmm11, xmm10
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm13, xmm10
  movdqa  xmm0, xmm4
  paddd   xmm0,  [r10+0x90]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm4
  palignr xmm7, xmm3, 0x04
  paddd   xmm5, xmm7
  sha256msg2 xmm5, xmm4
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm3, xmm4
  movdqa  xmm0, xmm11
  paddd   xmm0,  [r10+0x90]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm11
  palignr xmm14, xmm10, 0x04
  paddd   xmm12, xmm14
  sha256msg2 xmm12, xmm11
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm10, xmm11
  movdqa  xmm0, xmm5
  paddd   xmm0,  [r10+0xA0]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm5
  palignr xmm7, xmm4, 0x04
  paddd   xmm6, xmm7
  sha256msg2 xmm6, xmm5
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm4, xmm5
  movdqa  xmm0, xmm12
  paddd   xmm0,  [r10+0xA0]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm12
  palignr xmm14, xmm11, 0x04
  paddd   xmm13, xmm14
  sha256msg2 xmm13, xmm12
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm11, xmm12
  movdqa  xmm0, xmm6
  paddd   xmm0,  [r10+0xB0]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm6
  palignr xmm7, xmm5, 0x04
  paddd   xmm3, xmm7
  sha256msg2 xmm3, xmm6
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm5, xmm6
  movdqa  xmm0, xmm13
  paddd   xmm0,  [r10+0xB0]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm13
  palignr xmm14, xmm12, 0x04
  paddd   xmm10, xmm14
  sha256msg2 xmm10, xmm13
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm12, xmm13
  movdqa  xmm0, xmm3
  paddd   xmm0,  [r10+0xC0]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm3
  palignr xmm7, xmm6, 0x04
  paddd   xmm4, xmm7
  sha256msg2 xmm4, xmm3
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  sha256msg1 xmm6, xmm3
  movdqa  xmm0, xmm10
  paddd   xmm0,  [r10+0xC0]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm10
  palignr xmm14, xmm13, 0x04
  paddd   xmm11, xmm14
  sha256msg2 xmm11, xmm10
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  sha256msg1 xmm13, xmm10
  movdqa  xmm0, xmm4
  paddd   xmm0,  [r10+0xD0]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm4
  palignr xmm7, xmm3, 0x04
  paddd   xmm5, xmm7
  sha256msg2 xmm5, xmm4
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  movdqa  xmm0, xmm11
  paddd   xmm0,  [r10+0xD0]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm11
  palignr xmm14, xmm10, 0x04
  paddd   xmm12, xmm14
  sha256msg2 xmm12, xmm11
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  movdqa  xmm0, xmm5
  paddd   xmm0,  [r10+0xE0]
  sha256rnds2 xmm2, xmm1, xmm0
  movdqa  xmm7, xmm5
  palignr xmm7, xmm4, 0x04
  paddd   xmm6, xmm7
  sha256msg2 xmm6, xmm5
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  movdqa  xmm0, xmm12
  paddd   xmm0,  [r10+0xE0]
  sha256rnds2 xmm9, xmm8, xmm0
  movdqa  xmm14, xmm12
  palignr xmm14, xmm11, 0x04
  paddd   xmm13, xmm14
  sha256msg2 xmm13, xmm12
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  movdqa  xmm0, xmm6
  paddd   xmm0,  [r10+0xF0]
  sha256rnds2 xmm2, xmm1, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm1, xmm2, xmm0
  movdqa  xmm0, xmm13
  paddd   xmm0,  [r10+0xF0]
  sha256rnds2 xmm9, xmm8, xmm0
  pshufd  xmm0, xmm0, 0x0E
  sha256rnds2 xmm8, xmm9, xmm0
  paddd   xmm1,  [rsp]
  paddd   xmm2,  [rsp+0x10]
  paddd   xmm8,  [rsp+0x20]
  paddd   xmm9,  [rsp+0x30]
  add     r11, 16
  cmp     r8, 0
  jz      done
  sub     r13, 1
  jne     shaloop
  mov     r13, r9
  mov     r11, rdx
  jmp     shaloop

done:
  movdqa  xmm3, xmm1
  movdqa  xmm4, xmm8
  punpckhdq xmm1, xmm2
  punpckhdq xmm8, xmm9
  punpckldq xmm2, xmm3
  punpckldq xmm9, xmm4
  pshufd  xmm1, xmm1, 0x72
  pshufd  xmm2, xmm2, 0x27
  pshufd  xmm8, xmm8, 0x72
  pshufd  xmm9, xmm9, 0x27
  movdqa  xmm15,  [STATE_MASK+rip]
  pand    xmm2, xmm15
  pand    xmm9, xmm15
  movdqu   [rdi], xmm1
  movdqu   [rdi+0x10], xmm2
  movdqu   [rdi+0x20], xmm8
  movdqu   [rdi+0x30], xmm9
  lea     rsp, [rbp-16]
  pop     r13
  pop     r12
.cfi_restore            r13
.cfi_restore            r12
.cfi_def_cfa_register   rsp
  pop     rbp
.cfi_adjust_cfa_offset  -8
.cfi_restore            rbp
  ret
.cfi_endproc
.size   sha_ext_mbx2, . - sha_ext_mbx2

.SECTION .rodata

.ALIGN  16
FIRST_STATE:
  .quad  0x510E527F9B05688C
  .quad  0x6A09E667BB67AE85

SECOND_STATE:
  .quad  0x1F83D9AB5BE0CD19
  .quad  0x3C6EF372A54FF53A

STATE_MASK:
  .quad  0xFFFFFFFFFFFFFFFF
  .quad  0xFFFFFF3FFFFFFFFF

CONSTANTS:
  .byte  0x98, 0x2F, 0x8A, 0x42, 0x91, 0x44, 0x37, 0x71
  .byte  0xCF, 0xFB, 0xC0, 0xB5, 0xA5, 0xDB, 0xB5, 0xE9
  .byte  0x5B, 0xC2, 0x56, 0x39, 0xF1, 0x11, 0xF1, 0x59
  .byte  0xA4, 0x82, 0x3F, 0x92, 0xD5, 0x5E, 0x1C, 0xAB
  .byte  0x98, 0xAA, 0x07, 0xD8, 0x01, 0x5B, 0x83, 0x12
  .byte  0xBE, 0x85, 0x31, 0x24, 0xC3, 0x7D, 0x0C, 0x55
  .byte  0x74, 0x5D, 0xBE, 0x72, 0xFE, 0xB1, 0xDE, 0x80
  .byte  0xA7, 0x06, 0xDC, 0x9B, 0x74, 0xF1, 0x9B, 0xC1
  .byte  0xC1, 0x69, 0x9B, 0xE4, 0x86, 0x47, 0xBE, 0xEF
  .byte  0xC6, 0x9D, 0xC1, 0x0F, 0xCC, 0xA1, 0x0C, 0x24
  .byte  0x6F, 0x2C, 0xE9, 0x2D, 0xAA, 0x84, 0x74, 0x4A
  .byte  0xDC, 0xA9, 0xB0, 0x5C, 0xDA, 0x88, 0xF9, 0x76
  .byte  0x52, 0x51, 0x3E, 0x98, 0x6D, 0xC6, 0x31, 0xA8
  .byte  0xC8, 0x27, 0x03, 0xB0, 0xC7, 0x7F, 0x59, 0xBF
  .byte  0xF3, 0x0B, 0xE0, 0xC6, 0x47, 0x91, 0xA7, 0xD5
  .byte  0x51, 0x63, 0xCA, 0x06, 0x67, 0x29, 0x29, 0x14
  .byte  0x85, 0x0A, 0xB7, 0x27, 0x38, 0x21, 0x1B, 0x2E
  .byte  0xFC, 0x6D, 0x2C, 0x4D, 0x13, 0x0D, 0x38, 0x53
  .byte  0x54, 0x73, 0x0A, 0x65, 0xBB, 0x0A, 0x6A, 0x76
  .byte  0x2E, 0xC9, 0xC2, 0x81, 0x85, 0x2C, 0x72, 0x92
  .byte  0xA1, 0xE8, 0xBF, 0xA2, 0x4B, 0x66, 0x1A, 0xA8
  .byte  0x70, 0x8B, 0x4B, 0xC2, 0xA3, 0x51, 0x6C, 0xC7
  .byte  0x19, 0xE8, 0x92, 0xD1, 0x24, 0x06, 0x99, 0xD6
  .byte  0x85, 0x35, 0x0E, 0xF4, 0x70, 0xA0, 0x6A, 0x10
  .byte  0x16, 0xC1, 0xA4, 0x19, 0x08, 0x6C, 0x37, 0x1E
  .byte  0x4C, 0x77, 0x48, 0x27, 0xB5, 0xBC, 0xB0, 0x34
  .byte  0xB3, 0x0C, 0x1C, 0x39, 0x4A, 0xAA, 0xD8, 0x4E
  .byte  0x4F, 0xCA, 0x9C, 0x5B, 0xF3, 0x6F, 0x2E, 0x68
  .byte  0xEE, 0x82, 0x8F, 0x74, 0x6F, 0x63, 0xA5, 0x78
  .byte  0x14, 0x78, 0xC8, 0x84, 0x08, 0x02, 0xC7, 0x8C
  .byte  0xFA, 0xFF, 0xBE, 0x90, 0xEB, 0x6C, 0x50, 0xA4
  .byte  0xF7, 0xA3, 0xF9, 0xBE, 0xF2, 0x78, 0x71, 0xC6

.section	.note.GNU-stack,"",@progbits
.section	.note.gnu.property,"a",@note
	.long	4,2f-1f,5
	.byte	0x47,0x4E,0x55,0
1:	.long	0xc0000002,4,3
.align	8
2:

.att_syntax prefix
