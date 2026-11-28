//go:build windows

package main

import (
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32DLL               = syscall.NewLazyDLL("user32.dll")
	gdi32DLL                = syscall.NewLazyDLL("gdi32.dll")
	procGetDC               = user32DLL.NewProc("GetDC")
	procReleaseDC           = user32DLL.NewProc("ReleaseDC")
	procEnumFontFamiliesW   = gdi32DLL.NewProc("EnumFontFamiliesExW")
	procCreateFontIndirectW = gdi32DLL.NewProc("CreateFontIndirectW")
	procSelectObject        = gdi32DLL.NewProc("SelectObject")
	procDeleteObject        = gdi32DLL.NewProc("DeleteObject")
	procGetGlyphIndicesW    = gdi32DLL.NewProc("GetGlyphIndicesW")
)

type logFontW struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

var arabicShapedProbe = func() []uint16 {
	out := make([]uint16, 0, 0xFEFC-0xFE80+1)
	for cp := uint16(0xFE80); cp <= 0xFEFC; cp++ {
		out = append(out, cp)
	}
	return out
}()

var (
	fontEnumMu       sync.Mutex
	fontEnumSeen     map[string]bool
	fontListCache    []string
	allFontListCache []string
	enumFontProc     = syscall.NewCallback(func(lf *logFontW, _ uintptr, _ uintptr, _ uintptr) uintptr {
		name := windows.UTF16ToString(lf.FaceName[:])
		if name != "" && !strings.HasPrefix(name, "@") {
			fontEnumSeen[name] = true
		}
		return 1
	})
)

func fontSupportsArabicShaped(hdc uintptr, family string) bool {
	var lf logFontW
	lf.CharSet = 1 // DEFAULT_CHARSET
	encoded := utf16.Encode([]rune(family))
	if len(encoded) > len(lf.FaceName)-1 {
		encoded = encoded[:len(lf.FaceName)-1]
	}
	copy(lf.FaceName[:], encoded)

	hfont, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
	if hfont == 0 {
		return false
	}
	defer procDeleteObject.Call(hfont)

	old, _, _ := procSelectObject.Call(hdc, hfont)
	if old == 0 {
		return false
	}
	defer procSelectObject.Call(hdc, old)

	var indices [0xFEFC - 0xFE80 + 1]uint16
	const ggiMarkNonexistingGlyphs = 0x0001
	ret, _, _ := procGetGlyphIndicesW.Call(
		hdc,
		uintptr(unsafe.Pointer(&arabicShapedProbe[0])),
		uintptr(len(arabicShapedProbe)),
		uintptr(unsafe.Pointer(&indices[0])),
		ggiMarkNonexistingGlyphs,
	)
	if uint32(ret) == 0xFFFFFFFF {
		return false
	}
	missing := 0
	for _, idx := range indices {
		if idx == 0xFFFF {
			missing++
			if missing > 2 {
				return false
			}
		}
	}
	return true
}

func listSystemFonts(filterArabic bool) []string {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return []string{"Arial", "Segoe UI", "Tahoma"}
	}
	defer procReleaseDC.Call(0, hdc)

	fontEnumSeen = map[string]bool{}
	lf := logFontW{CharSet: 1} // DEFAULT_CHARSET
	procEnumFontFamiliesW.Call(hdc, uintptr(unsafe.Pointer(&lf)), enumFontProc, 0, 0)
	out := make([]string, 0, len(fontEnumSeen))
	for n := range fontEnumSeen {
		if !filterArabic || fontSupportsArabicShaped(hdc, n) {
			out = append(out, n)
		}
	}
	fontEnumSeen = nil

	if len(out) == 0 {
		return []string{"Arial", "Segoe UI", "Tahoma"}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// ListFonts returns installed Windows font family names with complete Arabic glyph support.
func (a *App) ListFonts() []string {
	fontEnumMu.Lock()
	defer fontEnumMu.Unlock()

	if len(fontListCache) > 0 {
		cp := make([]string, len(fontListCache))
		copy(cp, fontListCache)
		return cp
	}

	out := listSystemFonts(true)
	fontListCache = make([]string, len(out))
	copy(fontListCache, out)
	return out
}

// ListAllFonts returns all installed Windows font family names without Arabic glyph filtering.
func (a *App) ListAllFonts() []string {
	fontEnumMu.Lock()
	defer fontEnumMu.Unlock()

	if len(allFontListCache) > 0 {
		cp := make([]string, len(allFontListCache))
		copy(cp, allFontListCache)
		return cp
	}

	out := listSystemFonts(false)
	allFontListCache = make([]string, len(out))
	copy(allFontListCache, out)
	return out
}
