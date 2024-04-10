package search

import (
	"regexp"
)

// U+3040 - U+30FF: hiragana and katakana (Japanese only)
// U+FF66 - U+FF9F: half-width katakana (Japanese only)
var japaneseRegex = regexp.MustCompile(`[\x{3040}-\x{30ff}\x{ff66}-\x{ff9f}]`)

// helper to check if an input string contains any Japanese-specific characters (hiragana or katakana). will not trigger on CJK characters which are not specific to Japanese
func containsJapanese(text string) bool {
	return japaneseRegex.MatchString(text)
}
