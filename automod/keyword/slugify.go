package keyword

import (
	"regexp"
	"strings"
)

var nonSlugChars = regexp.MustCompile(`[^\pL\pN]+`)

// Takes an arbitrary string (eg, an identifier or free-form text) and returns a version with all non-letter, non-digit characters removed, and all lower-case
func Slugify(orig string) string {
	return strings.ToLower(nonSlugChars.ReplaceAllString(orig, ""))
}
