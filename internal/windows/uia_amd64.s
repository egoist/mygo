//go:build windows

#include "textflag.h"

// UI Automation passes doubles in XMM registers, which Go callbacks do
// not read: these thunks copy their bits to the integer registers of the
// same arguments, then go on to the callbacks.

// IRawElementProviderFragmentRoot::ElementProviderFromPoint(this, x, y, out)
TEXT ·uiaFromPointThunk(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X1, DX
	MOVQ X2, R8
	MOVQ ·uiaFromPointCallback(SB), AX
	JMP  AX

// IRangeValueProvider::SetValue(this, value)
TEXT ·uiaSetValueThunk(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ X1, DX
	MOVQ ·uiaSetValueCallback(SB), AX
	JMP  AX

// func uiaThunks() (fromPoint, setValue uintptr)
TEXT ·uiaThunks(SB), NOSPLIT, $0-16
	LEAQ ·uiaFromPointThunk(SB), AX
	MOVQ AX, fromPoint+0(FP)
	LEAQ ·uiaSetValueThunk(SB), AX
	MOVQ AX, setValue+8(FP)
	RET

// ITextProvider::RangeFromPoint(this, UiaPoint point, out).
// Windows x64 passes the 16-byte point by reference.
TEXT ·uiaTextPointThunk(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ R8, R9
	MOVQ 8(DX), R8
	MOVQ (DX), DX
	MOVQ ·uiaTextPointCallback(SB), AX
	JMP AX

TEXT ·uiaTextPointEntry(SB), NOSPLIT, $0-8
	LEAQ ·uiaTextPointThunk(SB), AX
	MOVQ AX, ret+0(FP)
	RET
