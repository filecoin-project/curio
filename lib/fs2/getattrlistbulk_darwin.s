//go:build darwin

#include "textflag.h"

TEXT libc_getattrlistbulk_trampoline<>(SB),NOSPLIT,$0-0
	JMP libc_getattrlistbulk(SB)

GLOBL ·libcGetAttrListBulkAddr(SB), RODATA, $8
DATA ·libcGetAttrListBulkAddr(SB)/8, $libc_getattrlistbulk_trampoline<>(SB)
