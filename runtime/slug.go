package vibe

import (
	"strings"
	"unicode"
)

// maxSlug is the longest slug Slugify makes.
const maxSlug = 80

// translit spells letters outside a-z with a-z. It covers Azerbaijani and
// Turkish letters, other Latin letters with diacritics, and Cyrillic.
var translit = map[rune]string{
	'ə': "e", 'ş': "s", 'ç': "c", 'ğ': "g", 'ı': "i", 'ö': "o", 'ü': "u",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'æ': "ae", 'ć': "c", 'č': "c", 'ĉ': "c", 'ċ': "c", 'ď': "d", 'đ': "d", 'ð': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ĝ': "g", 'ġ': "g", 'ģ': "g", 'ĥ': "h", 'ħ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i",
	'ĵ': "j", 'ķ': "k", 'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ŀ': "l", 'ł': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o", 'œ': "oe",
	'ŕ': "r", 'ŗ': "r", 'ř': "r", 'ś': "s", 'ŝ': "s", 'ș': "s", 'š': "s", 'ß': "ss",
	'ţ': "t", 'ț': "t", 'ť': "t", 'ŧ': "t", 'þ': "th",
	'ù': "u", 'ú': "u", 'û': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w", 'ý': "y", 'ÿ': "y", 'ŷ': "y", 'ź': "z", 'ż': "z", 'ž': "z",

	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya", 'і': "i", 'ї': "yi", 'є': "ye", 'ґ': "g", 'ў': "u", 'ә': "e", 'ғ': "g",
	'қ': "q", 'ң': "n", 'ө': "o", 'ұ': "u", 'ү': "u", 'һ': "h", 'ј': "j",
}

// Slugify makes a URL-friendly slug from s: lowercase, transliterated,
// every run of other characters turned into one dash, trimmed, and at
// most 80 characters. "Salam Dünya!" becomes "salam-dunya". It returns ""
// when s has no letters or digits it can spell.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	put := func(t string) {
		if dash && b.Len() > 0 {
			b.WriteByte('-')
		}
		dash = false
		b.WriteString(t)
	}
	for _, r := range s {
		r = unicode.ToLower(r)
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			put(string(r))
		case unicode.Is(unicode.Mn, r):
			// A combining accent, as in a decomposed "ü", adds nothing.
		default:
			if t, ok := translit[r]; ok && t != "" {
				put(t)
			} else if !ok {
				dash = true
			}
		}
		if b.Len() >= maxSlug {
			break
		}
	}
	out := b.String()
	if len(out) > maxSlug {
		out = out[:maxSlug]
	}
	return strings.TrimRight(out, "-")
}

// ValidSlug reports whether s is a slug: groups of a-z and 0-9 joined by
// single dashes.
func ValidSlug(s string) bool {
	if s == "" || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && s[i-1] != '-':
		default:
			return false
		}
	}
	return true
}
