package search

import (
	"regexp"
)

// U+3131 - U+318E: Hangul Compatibility Jamo (Korean only)
// U+AC00 - U+D7A3: Hangul Syllables (Korean only)
var koreanRegex = regexp.MustCompile(`[\x{3131}-\x{318e}\x{ac00}-\x{d7a3}]`)

// Helper to check if an input string contains any Korean-specific characters (Hangul). Will not trigger on CJK characters which are not specific to Korean
func containsKorean(text string) bool {
	return koreanRegex.MatchString(text)
}
