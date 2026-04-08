package syntax

import "testing"

func BenchmarkParseURI(b *testing.B) {
	for b.Loop() {
		_, _ = ParseURI("https://example.com/path?query=1#fragment")
	}
	b.ReportAllocs()
}

func BenchmarkTIDClock_Next(b *testing.B) {
	clock := NewTIDClock(1)
	for b.Loop() {
		_ = clock.Next()
	}
	b.ReportAllocs()
}
