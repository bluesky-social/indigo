package keyword

import (
	"log/slog"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	puncChars                    = regexp.MustCompile(`[[:punct:]]+`)
	nonTokenChars                = regexp.MustCompile(`[^\pL\pN\s]+`)
	nonTokenCharsSkipCensorChars = regexp.MustCompile(`[^\pL\pN\s#*_-]`)
)

// Splits free-form text in to tokens, including lower-case, unicode normalization, and some unicode folding.
//
// The intent is for this to work similarly to an NLP tokenizer, as might be used in a fulltext search engine, and enable fast matching to a list of known tokens. It might eventually even do stemming, removing pluralization (trailing "s" for English), etc.
func TokenizeTextWithRegex(text string, nonTokenCharsRegex *regexp.Regexp) []string {
	// this function needs to be re-defined in every function call to prevent a race condition
	normFunc := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	split := strings.ToLower(nonTokenCharsRegex.ReplaceAllString(text, " "))
	bare := strings.ToLower(nonTokenCharsRegex.ReplaceAllString(split, ""))
	norm, _, err := transform.String(normFunc, bare)
	if err != nil {
		slog.Warn("unicode normalization error", "err", err)
		norm = bare
	}
	return strings.Fields(norm)
}

func TokenizeText(text string) []string {
	return TokenizeTextWithRegex(text, nonTokenChars)
}

func TokenizeTextSkippingCensorChars(text string) []string {
	return TokenizeTextWithRegex(text, nonTokenCharsSkipCensorChars)
}

func splitIdentRune(c rune) bool {
	return !unicode.IsLetter(c) && !unicode.IsNumber(c)
}

// Splits an identifier in to tokens. Removes any single-character tokens.
//
// For example, the-handle.bsky.social would be split in to ["the", "handle", "bsky", "social"]
func TokenizeIdentifier(orig string) []string {
	fields := strings.FieldsFunc(orig, splitIdentRune)
	out := make([]string, 0, len(fields))
	for _, v := range fields {
		tok := Slugify(v)
		if len(tok) > 1 {
			out = append(out, tok)
		}
	}
	return out
}
