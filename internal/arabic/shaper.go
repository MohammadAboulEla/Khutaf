// Package arabic converts logical Arabic text into pre-shaped, visually ordered
// text that renders correctly in game engines that only draw glyphs left-to-right
// without contextual joining.
package arabic

// forms maps a base Arabic letter to its Presentation Forms:
// (Isolated), (Isolated, Final) or (Isolated, Final, Initial, Medial).
var forms = map[rune][]rune{
	0x0621: {0xFE80},
	0x0622: {0xFE81, 0xFE82},
	0x0623: {0xFE83, 0xFE84},
	0x0624: {0xFE85, 0xFE86},
	0x0625: {0xFE87, 0xFE88},
	0x0626: {0xFE89, 0xFE8A, 0xFE8B, 0xFE8C},
	0x0627: {0xFE8D, 0xFE8E},
	0x0628: {0xFE8F, 0xFE90, 0xFE91, 0xFE92},
	0x0629: {0xFE93, 0xFE94},
	0x062A: {0xFE95, 0xFE96, 0xFE97, 0xFE98},
	0x062B: {0xFE99, 0xFE9A, 0xFE9B, 0xFE9C},
	0x062C: {0xFE9D, 0xFE9E, 0xFE9F, 0xFEA0},
	0x062D: {0xFEA1, 0xFEA2, 0xFEA3, 0xFEA4},
	0x062E: {0xFEA5, 0xFEA6, 0xFEA7, 0xFEA8},
	0x062F: {0xFEA9, 0xFEAA},
	0x0630: {0xFEAB, 0xFEAC},
	0x0631: {0xFEAD, 0xFEAE},
	0x0632: {0xFEAF, 0xFEB0},
	0x0633: {0xFEB1, 0xFEB2, 0xFEB3, 0xFEB4},
	0x0634: {0xFEB5, 0xFEB6, 0xFEB7, 0xFEB8},
	0x0635: {0xFEB9, 0xFEBA, 0xFEBB, 0xFEBC},
	0x0636: {0xFEBD, 0xFEBE, 0xFEBF, 0xFEC0},
	0x0637: {0xFEC1, 0xFEC2, 0xFEC3, 0xFEC4},
	0x0638: {0xFEC5, 0xFEC6, 0xFEC7, 0xFEC8},
	0x0639: {0xFEC9, 0xFECA, 0xFECB, 0xFECC},
	0x063A: {0xFECD, 0xFECE, 0xFECF, 0xFED0},
	0x0640: {0x0640, 0x0640, 0x0640, 0x0640}, // Tatweel
	0x0641: {0xFED1, 0xFED2, 0xFED3, 0xFED4},
	0x0642: {0xFED5, 0xFED6, 0xFED7, 0xFED8},
	0x0643: {0xFED9, 0xFEDA, 0xFEDB, 0xFEDC},
	0x0644: {0xFEDD, 0xFEDE, 0xFEDF, 0xFEE0},
	0x0645: {0xFEE1, 0xFEE2, 0xFEE3, 0xFEE4},
	0x0646: {0xFEE5, 0xFEE6, 0xFEE7, 0xFEE8},
	0x0647: {0xFEE9, 0xFEEA, 0xFEEB, 0xFEEC},
	0x0648: {0xFEED, 0xFEEE},
	0x0649: {0xFEEF, 0xFEF0},
	0x064A: {0xFEF1, 0xFEF2, 0xFEF3, 0xFEF4},
	// Persian / Urdu / Arabic extensions (Presentation Forms-A)
	0x0671: {0xFB50, 0xFB51},
	0x067E: {0xFB56, 0xFB57, 0xFB58, 0xFB59},
	0x0686: {0xFB7A, 0xFB7B, 0xFB7C, 0xFB7D},
	0x0698: {0xFB8A, 0xFB8B},
	0x06A4: {0xFB6A, 0xFB6B, 0xFB6C, 0xFB6D},
	0x06A9: {0xFB8E, 0xFB8F, 0xFB90, 0xFB91},
	0x06AF: {0xFB92, 0xFB93, 0xFB94, 0xFB95},
	0x06CC: {0xFBFC, 0xFBFD, 0xFBFE, 0xFBFF},
}

// lamAlef maps the Alef variant following a Lam to its (Isolated, Final) ligature.
var lamAlef = map[rune][2]rune{
	0x0622: {0xFEF5, 0xFEF6},
	0x0623: {0xFEF7, 0xFEF8},
	0x0625: {0xFEF9, 0xFEFA},
	0x0627: {0xFEFB, 0xFEFC},
}

const lam rune = 0x0644

// IsDiacritic reports whether r is a transparent Arabic mark (Tashkeel / Quranic marks).
func IsDiacritic(r rune) bool {
	return (r >= 0x064B && r <= 0x065F) || r == 0x0670 || (r >= 0x06D6 && r <= 0x06ED)
}

// HasArabic reports whether s contains any Arabic-script code point.
func HasArabic(s string) bool {
	for _, r := range s {
		if (r >= 0x0600 && r <= 0x06FF) || (r >= 0x0750 && r <= 0x077F) || (r >= 0x08A0 && r <= 0x08FF) {
			return true
		}
	}
	return false
}

// Shape replaces Arabic letters with their contextual Presentation Forms and
// applies mandatory Lam-Alef ligatures. Output stays in logical order;
// diacritics stay attached after their base letter.
func Shape(line string) string {
	if !HasArabic(line) {
		return line
	}
	src := []rune(line)
	n := len(src)
	out := make([]rune, 0, n)
	prevConnectsLeft := false

	for i := 0; i < n; {
		ch := src[i]
		if IsDiacritic(ch) {
			out = append(out, ch)
			i++
			continue
		}
		letterForms, ok := forms[ch]
		if !ok {
			out = append(out, ch)
			prevConnectsLeft = false
			i++
			continue
		}

		// Collect diacritics that belong to this letter.
		j := i + 1
		for j < n && IsDiacritic(src[j]) {
			j++
		}
		marks := src[i+1 : j]

		if ch == lam && j < n {
			if lig, isLig := lamAlef[src[j]]; isLig {
				idx := 0
				if prevConnectsLeft {
					idx = 1
				}
				out = append(out, lig[idx])
				out = append(out, marks...)
				k := j + 1
				for k < n && IsDiacritic(src[k]) {
					out = append(out, src[k])
					k++
				}
				prevConnectsLeft = false // Alef never joins to the left
				i = k
				continue
			}
		}

		nextConnectsRight := false
		if j < n {
			if nf, ok := forms[src[j]]; ok && len(nf) >= 2 {
				nextConnectsRight = true
			}
		}

		var idx int
		switch len(letterForms) {
		case 1:
			idx = 0
			prevConnectsLeft = false
		case 2:
			if prevConnectsLeft {
				idx = 1
			}
			prevConnectsLeft = false
		default:
			switch {
			case prevConnectsLeft && nextConnectsRight:
				idx = 3
			case nextConnectsRight:
				idx = 2
			case prevConnectsLeft:
				idx = 1
			}
			prevConnectsLeft = nextConnectsRight
		}
		out = append(out, letterForms[idx])
		out = append(out, marks...)
		i = j
	}
	return string(out)
}
