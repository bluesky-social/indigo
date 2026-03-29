package search

import (
	"regexp"
)

// U+1100 - U+11FF: Hangul Jamo (Korean only)
// U+3130 - U+318F: Hangul Compatibility Jamo (Korean only)
// U+A960 - U+A97F: Hangul Jamo Extended-A (Korean only)
// U+AC00 - U+D7A3: Hangul Syllables (Korean only)
// U+D7B0 - U+D7FF: Hangul Jamo Extended-B (Korean only)
var koreanRegex = regexp.MustCompile(`[\x{1100}-\x{11ff}\x{3130}-\x{318f}\x{a960}-\x{a97f}\x{ac00}-\x{d7a3}\x{d7b0}-\x{d7ff}]`)

// Helper to check if an input string contains any Korean-specific characters (Hangul). Will not trigger on CJK characters which are not specific to Korean
func containsKorean(text string) bool {
	return koreanRegex.MatchString(text)
}
