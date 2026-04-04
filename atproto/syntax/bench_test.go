package syntax

import "testing"

func BenchmarkParseDID(b *testing.B) {
	for b.Loop() {
		_, _ = ParseDID("did:plc:z72i7hdynmk6r22z27h6tvur")
	}
}

func BenchmarkParseHandle(b *testing.B) {
	for b.Loop() {
		_, _ = ParseHandle("alice.bsky.social")
	}
}

func BenchmarkParseNSID(b *testing.B) {
	for b.Loop() {
		_, _ = ParseNSID("app.bsky.feed.post")
	}
}

func BenchmarkParseATURI(b *testing.B) {
	for b.Loop() {
		_, _ = ParseATURI("at://did:plc:z72i7hdynmk6r22z27h6tvur/app.bsky.feed.post/3jt5tsfbx2s2a")
	}
}

func BenchmarkParseTID(b *testing.B) {
	for b.Loop() {
		_, _ = ParseTID("3jt5tsfbx2s2a")
	}
}

func BenchmarkParseRecordKey(b *testing.B) {
	for b.Loop() {
		_, _ = ParseRecordKey("3jt5tsfbx2s2a")
	}
}

func BenchmarkParseDatetime(b *testing.B) {
	for b.Loop() {
		_, _ = ParseDatetime("2024-01-15T12:00:00.123Z")
	}
}

func BenchmarkParseLanguage(b *testing.B) {
	for b.Loop() {
		_, _ = ParseLanguage("en-US")
	}
}

func BenchmarkParseURI(b *testing.B) {
	for b.Loop() {
		_, _ = ParseURI("https://example.com/path?query=1#fragment")
	}
}

func BenchmarkParseCID(b *testing.B) {
	for b.Loop() {
		_, _ = ParseCID("bafyreidfayvfkwc6e3jbv7my3lqofezflhzhqdrxtmu6nzgpmvaqfuo2qq")
	}
}

func BenchmarkTIDClock_Next(b *testing.B) {
	clock := NewTIDClock(1)
	for b.Loop() {
		_ = clock.Next()
	}
}
