package keyword

import (
	"regexp"
	"sync"
)

var substitutions = map[rune]string{
	'a': "aÁáÀàĂăẮắẰằẴẵẲẳÂâẤấẦầẪẫẨẩǍǎÅåǺǻÄäǞǟÃãȦȧǠǡĄąĄ́ą́Ą̃ą̃ĀāĀ̀ā̀ẢảȀȁA̋a̋ȂȃẠạẶặẬậḀḁȺⱥꞺꞻᶏẚＡａ@4",
	'b': "",
	'c': "cĆćĈĉČčĊċÇçḈḉȻȼꞒꞓꟄꞔƇƈɕ",
	'd': "",
	'e': "e3ЄєЕеÉéÈèĔĕÊêẾếỀềỄễỂểÊ̄ê̄Ê̌ê̌ĚěËëẼẽĖėĖ́ė́Ė̃ė̃ȨȩḜḝ ĘęĘ́ę́Ę̃ę̃ĒēḖḗḔḕẺẻȄȅE̋e̋ȆȇẸẹỆệḘḙḚḛɆɇE̩e̩È̩è̩É̩é̩ᶒⱸꬴꬳＥｅ",
	'f': "fḞḟƑƒꞘꞙᵮᶂ",
	'g': "gǴǵĞğĜĝǦǧĠġG̃g̃ĢģḠḡǤǥꞠꞡƓɠᶃꬶＧｇqꝖꝗꝘꝙɋʠ",
	'h': "hĤĥȞȟḦḧḢḣḨḩḤḥḪḫH̱ẖĦħⱧⱨꞪɦꞕΗНн",
	'i': "iÍíi̇́Ììi̇̀ĬĭÎîǏǐÏïḮḯĨĩi̇̃ĮįĮ́į̇́Į̃į̇̃ĪīĪ̀ī̀ỈỉȈȉI̋i̋ȊȋỊịꞼꞽḬḭƗɨᶖİiIıＩｉ1lĺľļḷḹl̃ḽḻłŀƚꝉⱡɫɬꞎꬷꬸꬹᶅɭȴＬｌ",
	'j': "",
	'k': "[kḰḱǨǩĶķḲḳḴḵƘƙⱩⱪᶄꝀꝁꝂꝃꝄꝅꞢꞣ",
	'l': "", // omitted because it's always substituted to 'i'.
	'm': "",
	'n': "nŃńǸǹŇňÑñṄṅŅņṆṇṊṋṈṉN̈n̈ƝɲŊŋꞐꞑꞤꞥᵰᶇɳȵꬻꬼИиПпＮｎ",
	'o': "ÓóÒòŎŏÔôỐốỒồỖỗỔổǑǒÖöȪȫŐőÕõṌṍṎṏȬȭȮȯO͘o͘ȰȱØøǾǿǪǫǬǭŌōṒṓṐṑỎỏȌȍȎȏƠơỚớỜờỠỡỞởỢợỌọỘộO̩o̩Ò̩ò̩Ó̩ó̩ƟɵꝊꝋꝌꝍⱺＯｏ0",
	'p': "",
	'q': "",
	'r': "rŔŕŘřṘṙŖŗȐȑȒȓṚṛṜṝṞṟR̃r̃ɌɍꞦꞧⱤɽᵲᶉꭉ",
	's': "sŚśṤṥŜŝŠšṦṧṠṡŞşṢṣṨṩȘșS̩s̩ꞨꞩⱾȿꟅʂᶊᵴ",
	't': "tŤťṪṫŢţṬṭȚțṰṱṮṯŦŧȾⱦƬƭƮʈT̈ẗᵵƫȶ",
	'u': "",
	'v': "",
	'w': "",
	'x': "",
	'y': "yÝýỲỳŶŷY̊ẙŸÿỸỹẎẏȲȳỶỷỴỵɎɏƳƴỾỿ",
	'z': "",
}

var substitutionsProcessed map[rune]rune
var substitutionsOnce sync.Once

func Normalize(raw string) string {
	substitutionsOnce.Do(func() {
		substitutionsProcessed := make(map[rune]rune)
		for sub, chars := range substitutions {
			for _, k := range chars {
				substitutionsProcessed[k] = sub
			}
		}
	})

	ret := make([]rune, len([]rune(raw)))
	copy(ret, []rune(raw))
	for i, v := range raw {
		if sub, ok := substitutionsProcessed[v]; ok {
			ret[i] = sub
		}
	}
	return string(ret)
}

// regexes taken from: https://github.com/Blank-Cheque/Slurs
var explicitSlurRegexes = map[string]*regexp.Regexp{
	"chink": regexp.MustCompile("chinks?"),
	// modified to not match "cocoon", "raccoon", "racoon", or "tycoon"
	"coon":   regexp.MustCompile("(^|[^cayo])coons?"),
	"faggot": regexp.MustCompile("fagg[oei]t{1,2}(ry|rie?)?s?"),
	"kike":   regexp.MustCompile("k[iy]ke(ry|rie)?s*"),
	// modified to not match "snigger"
	"nigger": regexp.MustCompile("(^|[^s])n[ioa]gg([ea]r?|nog|a)s?"),
	"tranny": regexp.MustCompile("tra+n{1,2}(ie|y|er)s?"),
}

// For a small set of frequently-abused explicit slurs, checks for a of permissive set of "l33t-speak" variations of the keyword. This is intended to be used with pre-processed "slugs", which are strings with all whitespace, punctuation, and other characters removed. These could be pre-processed identifiers (like handles or record keys), or pre-processed free-form text.
//
// If there is a match, returns a plan-text version of the slur.
//
// This is a loose port of the 'hasExplicitSlur' function from the `@atproto/pds` TypeScript package.
func SlugContainsExplicitSlur(raw string) string {
	raw = Normalize(raw)
	for word, r := range explicitSlurRegexes {
		if r.MatchString(raw) {
			return word
		}
	}
	return ""
}

// Variant of `SlugContainsExplicitSlur` where the entire slug must match.
func SlugIsExplicitSlur(raw string) string {
	raw = Normalize(raw)
	for word, r := range explicitSlurRegexes {
		m := r.FindString(raw)
		if m != "" && m == raw {
			return word
		}
	}
	return ""
}
