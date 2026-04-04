package syntax

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDID_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Max length DID (2048 chars).
	longID := "did:plc:" + strings.Repeat("a", 2040)
	_, err := ParseDID(longID)
	assert.NoError(err)

	// One over max.
	tooLong := "did:plc:" + strings.Repeat("a", 2041)
	_, err = ParseDID(tooLong)
	assert.Error(err)

	// Ends with '%' — invalid.
	_, err = ParseDID("did:plc:abc%")
	assert.Error(err)

	// Ends with ':' — invalid.
	_, err = ParseDID("did:plc:abc:")
	assert.Error(err)

	// Uppercase DID prefix.
	_, err = ParseDID("DID:plc:abc123")
	assert.Error(err)

	// Uppercase method.
	_, err = ParseDID("did:PLC:abc123")
	assert.Error(err)

	// Digits in method.
	_, err = ParseDID("did:pl1c:abc123")
	assert.Error(err)

	// No identifier after method.
	_, err = ParseDID("did:plc:")
	assert.Error(err)

	// Just "did:".
	_, err = ParseDID("did:")
	assert.Error(err)

	// did:plc: fast path — exactly 32 chars, valid.
	d, err := ParseDID("did:plc:z234567890abcdefghijklmn")
	assert.NoError(err)
	assert.Equal("plc", d.Method())

	// did:plc: with dots in identifier — valid DID syntax, even if not specifically for did plc
	_, err = ParseDID("did:example:z234567890abcdef.hijklmn")
	assert.NoError(err)

	// Valid percent-encoding.
	_, err = ParseDID("did:example:abc%2Fdef")
	assert.NoError(err)

	// Invalid percent-encoding: non-hex digits after %.
	_, err = ParseDID("did:example:abc%zzdef")
	assert.Error(err)

	// Truncated percent-encoding: % at end minus one.
	_, err = ParseDID("did:example:abc%2")
	assert.Error(err)

	// Zero-value DID returns empty for Method/Identifier.
	var zero DID
	assert.Equal("", zero.Method())
	assert.Equal("", zero.Identifier())
}

func TestHandle_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Max total length (253 chars).
	long := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ".com"
	assert.Equal(253, len(long))
	_, err := ParseHandle(long)
	assert.NoError(err)

	// Single label — invalid.
	_, err = ParseHandle("localhost")
	assert.Error(err)

	// Underscore — invalid.
	_, err = ParseHandle("alice_bob.social")
	assert.Error(err)

	// Hyphen at start of label.
	_, err = ParseHandle("-alice.social")
	assert.Error(err)

	// Hyphen at end of label.
	_, err = ParseHandle("alice-.social")
	assert.Error(err)

	// TLD starts with digit.
	_, err = ParseHandle("alice.123")
	assert.Error(err)

	// Empty string.
	_, err = ParseHandle("")
	assert.Error(err)

	// Double dot.
	_, err = ParseHandle("alice..social")
	assert.Error(err)
}

func TestNSID_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Two segments only — invalid.
	_, err := ParseNSID("com.example")
	assert.Error(err)

	// Name segment starts with digit — invalid.
	_, err = ParseNSID("com.example.1foo")
	assert.Error(err)

	// Name segment with hyphen — invalid.
	_, err = ParseNSID("com.example.foo-bar")
	assert.Error(err)

	// Name segment with underscore — invalid.
	_, err = ParseNSID("com.example.foo_bar")
	assert.Error(err)

	// Empty string.
	_, err = ParseNSID("")
	assert.Error(err)

	// Max length (317).
	seg := strings.Repeat("a", 63)
	nsid := seg + "." + seg + "." + seg + "." + seg + "." + "fooBar"
	assert.LessOrEqual(len(nsid), 317)
	_, err = ParseNSID(nsid)
	assert.NoError(err)
}

func TestATURI_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Authority only.
	a, err := ParseATURI("at://did:plc:abc123")
	assert.NoError(err)
	assert.Equal(NSID(""), a.Collection())
	assert.Equal(RecordKey(""), a.RecordKey())

	// Trailing slash — invalid.
	_, err = ParseATURI("at://did:plc:abc123/")
	assert.Error(err)

	// Double trailing slash — invalid.
	_, err = ParseATURI("at://did:plc:abc123/com.example.foo/")
	assert.Error(err)

	// Fragment — invalid.
	_, err = ParseATURI("at://did:plc:abc123#frag")
	assert.Error(err)

	// Query — invalid.
	_, err = ParseATURI("at://did:plc:abc123?query")
	assert.Error(err)

	// Too many segments — invalid.
	_, err = ParseATURI("at://did:plc:abc123/com.example.foo/rkey/extra")
	assert.Error(err)

	// Wrong scheme.
	_, err = ParseATURI("http://did:plc:abc123")
	assert.Error(err)

	// Empty authority.
	_, err = ParseATURI("at://")
	assert.Error(err)
}

func TestTID_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Wrong length.
	_, err := ParseTID("222222222222") // 12 chars
	assert.Error(err)
	_, err = ParseTID("22222222222222") // 14 chars
	assert.Error(err)

	// High bit set in first char (k-z) — invalid.
	_, err = ParseTID("k222222222222")
	assert.Error(err)
	_, err = ParseTID("z222222222222")
	assert.Error(err)

	// All '2' is valid.
	_, err = ParseTID("2222222222222")
	assert.NoError(err)

	// Uppercase — invalid.
	_, err = ParseTID("222222222222A")
	assert.Error(err)

	// Digits 0 and 1 not in alphabet.
	_, err = ParseTID("2222222222220")
	assert.Error(err)
	_, err = ParseTID("2222222222221")
	assert.Error(err)
}

func TestRecordKey_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Max length (512).
	long := strings.Repeat("a", 512)
	_, err := ParseRecordKey(long)
	assert.NoError(err)

	// One over max.
	_, err = ParseRecordKey(strings.Repeat("a", 513))
	assert.Error(err)

	// Slash — invalid.
	_, err = ParseRecordKey("abc/def")
	assert.Error(err)

	// Space — invalid.
	_, err = ParseRecordKey("abc def")
	assert.Error(err)

	// Various allowed special chars.
	_, err = ParseRecordKey("abc_def~ghi.jkl:mno-pqr")
	assert.NoError(err)
}

func TestDatetime_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Timezone +00:00 is fine.
	_, err := ParseDatetime("1985-04-12T23:20:50.123+00:00")
	assert.NoError(err)

	// Z is fine.
	_, err = ParseDatetime("1985-04-12T23:20:50.123Z")
	assert.NoError(err)

	// -00:00 rejected.
	_, err = ParseDatetime("1985-04-12T23:20:50.123-00:00")
	assert.Error(err)

	// No fractional seconds — valid.
	_, err = ParseDatetime("1985-04-12T23:20:50Z")
	assert.NoError(err)

	// Lowercase 'z' — invalid.
	_, err = ParseDatetime("1985-04-12T23:20:50.123z")
	assert.Error(err)

	// Space instead of T — invalid.
	_, err = ParseDatetime("1985-04-12 23:20:50.123Z")
	assert.Error(err)

	// Incomplete timezone.
	_, err = ParseDatetime("1985-04-12T23:20:50.123+00:0")
	assert.Error(err)

	// No timezone — invalid.
	_, err = ParseDatetime("1985-04-12T23:20:50.123")
	assert.Error(err)

	// Leading/trailing space — invalid.
	_, err = ParseDatetime(" 1985-04-12T23:20:50.123Z")
	assert.Error(err)
	_, err = ParseDatetime("1985-04-12T23:20:50.123Z ")
	assert.Error(err)

	// Hour 24 rejected (valid in ISO 8601 but not RFC 3339).
	_, err = ParseDatetime("1985-04-12T24:00:00Z")
	assert.Error(err)

	// Leap year: Feb 29 valid.
	_, err = ParseDatetime("2024-02-29T00:00:00Z")
	assert.NoError(err)

	// Non-leap year: Feb 29 invalid.
	_, err = ParseDatetime("2023-02-29T00:00:00Z")
	assert.Error(err)

	// High-precision fractional seconds (20 digits max).
	_, err = ParseDatetime("1985-04-12T23:20:50." + strings.Repeat("1", 20) + "Z")
	assert.NoError(err)

	// Over 20 fractional digits — invalid.
	_, err = ParseDatetime("1985-04-12T23:20:50." + strings.Repeat("1", 21) + "Z")
	assert.Error(err)

	// Year 0000 is allowed.
	_, err = ParseDatetime("0000-01-01T00:00:00Z")
	assert.NoError(err)

	// Comma as fractional separator — invalid (only dot allowed).
	_, err = ParseDatetime("1985-04-12T23:20:50,123Z")
	assert.Error(err)

	// Lenient: -00:00 gets fixed.
	d, err := ParseDatetimeLenient("1985-04-12T23:20:50.123-00:00")
	assert.NoError(err)
	assert.Contains(d.String(), "+00:00")

	// Lenient: missing timezone gets Z appended.
	d, err = ParseDatetimeLenient("1985-04-12T23:20:50.123")
	assert.NoError(err)
	assert.Contains(d.String(), "Z")
}

func TestCID_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Min length (8).
	_, err := ParseCID("abcdefgh")
	assert.NoError(err)

	// Too short (7).
	_, err = ParseCID("abcdefg")
	assert.Error(err)

	// Max length (256).
	_, err = ParseCID(strings.Repeat("a", 256))
	assert.NoError(err)

	// One over max.
	_, err = ParseCID(strings.Repeat("a", 257))
	assert.Error(err)

	// Invalid character.
	_, err = ParseCID("abcdefgh!")
	assert.Error(err)

	// CIDv0 rejected.
	_, err = ParseCID("QmbWqxBEKC3P8tqsKc98xmWNzrzDtRLMiMPL8wBuTGsMnR")
	assert.Error(err)

	// + and = are valid CID chars.
	_, err = ParseCID("abcd+ef=")
	assert.NoError(err)
}

func TestURI_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Valid schemes.
	_, err := ParseURI("https://example.com")
	assert.NoError(err)
	_, err = ParseURI("at://did:plc:abc123")
	assert.NoError(err)

	// Uppercase scheme — invalid.
	_, err = ParseURI("HTTPS://example.com")
	assert.Error(err)

	// No colon — invalid.
	_, err = ParseURI("httpexample.com")
	assert.Error(err)

	// Empty body after colon — invalid.
	_, err = ParseURI("https:")
	assert.Error(err)

	// Whitespace in body — invalid.
	_, err = ParseURI("https://example.com/foo bar")
	assert.Error(err)
}

func TestLanguage_EdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Valid.
	_, err := ParseLanguage("en")
	assert.NoError(err)
	_, err = ParseLanguage("en-US")
	assert.NoError(err)
	_, err = ParseLanguage("i-klingon")
	assert.NoError(err)

	// Primary subtag too long (>3).
	_, err = ParseLanguage("engl")
	assert.Error(err)

	// Uppercase primary — invalid.
	_, err = ParseLanguage("EN")
	assert.Error(err)

	// Single non-'i' char — invalid.
	_, err = ParseLanguage("e")
	assert.Error(err)

	// Empty subtag after hyphen — invalid.
	_, err = ParseLanguage("en-")
	assert.Error(err)

	// Too long.
	_, err = ParseLanguage(strings.Repeat("en-", 50))
	assert.Error(err)

	// Non-alphanumeric in subtag.
	_, err = ParseLanguage("en-US_foo")
	assert.Error(err)

	// Hyphen where subtag expected (not hyphen separator).
	_, err = ParseLanguage("en-US-")
	assert.Error(err)
}

func TestURI_MoreEdgeCases(t *testing.T) {
	assert := assert.New(t)

	// Scheme too long (>80 chars before colon).
	longScheme := strings.Repeat("a", 82) + "://example.com"
	_, err := ParseURI(longScheme)
	assert.Error(err)

	// DEL character in body — invalid.
	_, err = ParseURI("https://example.com/\x7f")
	assert.Error(err)
}

// Test JSON marshal/unmarshal for types not covered by json_test.go.
func TestMarshalUnmarshalCoverage(t *testing.T) {
	assert := assert.New(t)

	// CID
	type CIDWrapper struct {
		C CID `json:"c"`
	}
	cidJSON := `{"c":"bafyreidfayvfkwc6e3jbv7my3lqofezflhzhqdrxtmu6nzgpmvaqfuo2qq"}`
	var cw CIDWrapper
	err := json.Unmarshal([]byte(cidJSON), &cw)
	assert.NoError(err)
	assert.Equal(CID("bafyreidfayvfkwc6e3jbv7my3lqofezflhzhqdrxtmu6nzgpmvaqfuo2qq"), cw.C)
	out, err := json.Marshal(cw)
	assert.NoError(err)
	assert.Contains(string(out), "bafyreidfayvfkwc6e3jbv7my3lqofezflhzhqdrxtmu6nzgpmvaqfuo2qq")

	// CID unmarshal error.
	badCID := `{"c":"bad!"}`
	err = json.Unmarshal([]byte(badCID), &cw)
	assert.Error(err)

	// Datetime
	type DTWrapper struct {
		D Datetime `json:"d"`
	}
	dtJSON := `{"d":"2024-01-15T12:00:00Z"}`
	var dw DTWrapper
	err = json.Unmarshal([]byte(dtJSON), &dw)
	assert.NoError(err)
	out, err = json.Marshal(dw)
	assert.NoError(err)
	assert.Contains(string(out), "2024-01-15T12:00:00Z")

	// Datetime unmarshal error.
	badDT := `{"d":"not-a-date"}`
	err = json.Unmarshal([]byte(badDT), &dw)
	assert.Error(err)

	// Language
	type LangWrapper struct {
		L Language `json:"l"`
	}
	langJSON := `{"l":"en-US"}`
	var lw LangWrapper
	err = json.Unmarshal([]byte(langJSON), &lw)
	assert.NoError(err)
	out, err = json.Marshal(lw)
	assert.NoError(err)
	assert.Contains(string(out), "en-US")

	// Language unmarshal error.
	badLang := `{"l":"NOPE"}`
	err = json.Unmarshal([]byte(badLang), &lw)
	assert.Error(err)

	// TID
	type TIDWrapper struct {
		T TID `json:"t"`
	}
	tidJSON := `{"t":"3jt5tsfbx2s2a"}`
	var tw TIDWrapper
	err = json.Unmarshal([]byte(tidJSON), &tw)
	assert.NoError(err)
	out, err = json.Marshal(tw)
	assert.NoError(err)
	assert.Contains(string(out), "3jt5tsfbx2s2a")

	// TID unmarshal error.
	badTID := `{"t":"nope"}`
	err = json.Unmarshal([]byte(badTID), &tw)
	assert.Error(err)

	// URI
	type URIWrapper struct {
		U URI `json:"u"`
	}
	uriJSON := `{"u":"https://example.com"}`
	var uw URIWrapper
	err = json.Unmarshal([]byte(uriJSON), &uw)
	assert.NoError(err)
	out, err = json.Marshal(uw)
	assert.NoError(err)
	assert.Contains(string(out), "https://example.com")

	// URI unmarshal error.
	badURI := `{"u":""}`
	err = json.Unmarshal([]byte(badURI), &uw)
	assert.Error(err)
}

func TestAtIdentifierConvenienceGetters(t *testing.T) {
	assert := assert.New(t)

	// Handle() on a DID returns empty.
	did, err := ParseAtIdentifier("did:plc:abc123")
	assert.NoError(err)
	assert.Equal(Handle(""), did.Handle())
	assert.Equal(DID("did:plc:abc123"), did.DID())

	// DID() on a Handle returns empty.
	handle, err := ParseAtIdentifier("example.com")
	assert.NoError(err)
	assert.Equal(DID(""), handle.DID())
	assert.Equal(Handle("example.com"), handle.Handle())
}

func TestHandleIsInvalidHandle(t *testing.T) {
	assert := assert.New(t)

	h, err := ParseHandle("handle.invalid")
	assert.NoError(err)
	assert.True(h.IsInvalidHandle())

	h2, err := ParseHandle("alice.bsky.social")
	assert.NoError(err)
	assert.False(h2.IsInvalidHandle())
}

func TestHandleAllowedTLD(t *testing.T) {
	assert := assert.New(t)

	// Allowed.
	h, _ := ParseHandle("alice.bsky.social")
	assert.True(h.AllowedTLD())

	h, _ = ParseHandle("alice.test")
	assert.True(h.AllowedTLD())

	// Disallowed TLDs.
	for _, tld := range []string{"local", "arpa", "invalid", "localhost", "internal", "example", "onion", "alt"} {
		h, err := ParseHandle("alice." + tld)
		assert.NoError(err, "should parse: alice.%s", tld)
		assert.False(h.AllowedTLD(), "should be disallowed: %s", tld)
	}
}

func TestClockFromTID(t *testing.T) {
	assert := assert.New(t)

	tid := NewTID(1234567890, 42)
	clk := ClockFromTID(tid)
	assert.Equal(uint(42), clk.ClockID)

	// Next TID from reconstructed clock should be greater.
	next := clk.Next()
	assert.Greater(next.String(), tid.String())
}

func TestBase32Sort(t *testing.T) {
	enc := Base32Sort()
	assert.NotNil(t, enc)
}

func TestATURINormalizeCollectionOnly(t *testing.T) {
	assert := assert.New(t)

	// AT-URI with collection but no record key — exercises the middle branch of Normalize.
	a, err := ParseATURI("at://did:plc:abc123/app.bsky.feed.post")
	assert.NoError(err)
	norm := a.Normalize()
	assert.Equal(ATURI("at://did:plc:abc123/app.bsky.feed.post"), norm)
}

func TestDatetimeTimeBadValue(t *testing.T) {
	// Datetime.Time() on an invalid (hand-constructed) Datetime returns zero time.
	bad := Datetime("not-a-datetime")
	assert.True(t, bad.Time().IsZero())
}

func TestDatetimeTooLong(t *testing.T) {
	_, err := ParseDatetime(strings.Repeat("2", 65))
	assert.Error(t, err)
}

// Test UnmarshalText error paths for types whose success path is tested via json_test.go.
func TestUnmarshalTextErrors(t *testing.T) {
	assert := assert.New(t)

	var d DID
	assert.Error(d.UnmarshalText([]byte("bad")))

	var n NSID
	assert.Error(n.UnmarshalText([]byte("bad")))

	var a ATURI
	assert.Error(a.UnmarshalText([]byte("bad")))

	var r RecordKey
	assert.Error(r.UnmarshalText([]byte("")))

	var ai AtIdentifier
	assert.Error(ai.UnmarshalText([]byte("")))
}

func TestParseURI_InvalidBodyChar(t *testing.T) {
	assert := assert.New(t)

	// Control character in body.
	_, err := ParseURI("https://example.com/\x01")
	assert.Error(err)

	// Tab in body.
	_, err = ParseURI("https://example.com/\t")
	assert.Error(err)
}

func TestDatetimeSyntax_MoreBranches(t *testing.T) {
	assert := assert.New(t)

	// Invalid year digit.
	_, err := ParseDatetime("20x4-01-15T12:00:00Z")
	assert.Error(err)

	// Invalid month digit.
	_, err = ParseDatetime("2024-x1-15T12:00:00Z")
	assert.Error(err)

	// Invalid day digit.
	_, err = ParseDatetime("2024-01-x5T12:00:00Z")
	assert.Error(err)

	// Invalid hour digit.
	_, err = ParseDatetime("2024-01-15Tx2:00:00Z")
	assert.Error(err)

	// Invalid minute digit.
	_, err = ParseDatetime("2024-01-15T12:x0:00Z")
	assert.Error(err)

	// Invalid second digit.
	_, err = ParseDatetime("2024-01-15T12:00:x0Z")
	assert.Error(err)

	// Missing hyphen after year.
	_, err = ParseDatetime("2024X01-15T12:00:00Z")
	assert.Error(err)

	// Missing hyphen after month.
	_, err = ParseDatetime("2024-01X15T12:00:00Z")
	assert.Error(err)

	// Missing colon after hour.
	_, err = ParseDatetime("2024-01-15T12X00:00Z")
	assert.Error(err)

	// Missing colon after minute.
	_, err = ParseDatetime("2024-01-15T12:00X00Z")
	assert.Error(err)

	// Dot with no fractional digits.
	_, err = ParseDatetime("2024-01-15T12:00:00.Z")
	assert.Error(err)

	// Invalid timezone hour digits.
	_, err = ParseDatetime("2024-01-15T12:00:00+xx:00")
	assert.Error(err)

	// Invalid timezone colon.
	_, err = ParseDatetime("2024-01-15T12:00:00+00x00")
	assert.Error(err)

	// Invalid timezone minute digits.
	_, err = ParseDatetime("2024-01-15T12:00:00+00:xx")
	assert.Error(err)

	// Truncated timezone.
	_, err = ParseDatetime("2024-01-15T12:00:00+00")
	assert.Error(err)

	// Invalid timezone char (not Z, +, or -).
	_, err = ParseDatetime("2024-01-15T12:00:00X")
	assert.Error(err)

	// Trailing chars after valid datetime.
	_, err = ParseDatetime("2024-01-15T12:00:00Zextra")
	assert.Error(err)
}

func TestHasTimezone_CompactOffset(t *testing.T) {
	// Exercise the +hhmm (without colon) branch in hasTimezone.
	// This is used by ParseDatetimeLenient.
	assert := assert.New(t)

	// +0000 suffix — lenient should fix it.
	d, err := ParseDatetimeLenient("2024-01-15T12:00:00+0000")
	assert.NoError(err)
	assert.Contains(d.String(), "+00:00")

	// -0000 suffix — lenient should fix it.
	d, err = ParseDatetimeLenient("2024-01-15T12:00:00-0000")
	assert.NoError(err)
	assert.Contains(d.String(), "+00:00")

	// +0530 — compact non-zero offset. This exercises the len>=5 branch in hasTimezone.
	// This is not fixable by lenient parsing (no colon in offset), so it should error.
	_, err = ParseDatetimeLenient("2024-01-15T12:00:00+0530")
	assert.Error(err)

	// A string with no timezone at all (hasTimezone returns false).
	d, err = ParseDatetimeLenient("2024-01-15T12:00:00")
	assert.NoError(err)
	assert.Contains(d.String(), "Z")

	// Empty string — hasTimezone returns false.
	_, err = ParseDatetimeLenient("")
	assert.Error(err)
}

func TestParseURI_SchemeInvalidChar(t *testing.T) {
	// Invalid character mid-scheme (not lowercase, digit, +, ., or -).
	_, err := ParseURI("h@tp://example.com")
	assert.Error(t, err)
}

func TestParseLanguage_NonAlphanumericMidSubtag(t *testing.T) {
	// Non-alphanumeric char mid-subtag.
	_, err := ParseLanguage("en-U!S")
	assert.Error(t, err)
}
