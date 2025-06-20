package relay

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type HostnameFixture struct {
	Val      string
	Error    bool
	Hostname string
	NoSSL    bool
}

func TestParseHostname(t *testing.T) {
	assert := assert.New(t)

	fixtures := []HostnameFixture{
		HostnameFixture{Val: "asdf", Error: true},
		HostnameFixture{Val: "https://pds.example.com", Hostname: "pds.example.com", NoSSL: false},
		HostnameFixture{Val: "http://pds.example.com", Hostname: "pds.example.com", NoSSL: true},
		HostnameFixture{Val: "ws://pds.example.com", Hostname: "pds.example.com", NoSSL: true},
		HostnameFixture{Val: "pds.example.com", Hostname: "pds.example.com", NoSSL: false},
		HostnameFixture{Val: "morel.us-east.host.gndr.network", Hostname: "morel.us-east.host.gndr.network", NoSSL: false},
		HostnameFixture{Val: "https://service.local", Hostname: "service.local", NoSSL: false}, // TODO: SSRF
		HostnameFixture{Val: "localhost:8080", Hostname: "localhost:8080", NoSSL: true},
		HostnameFixture{Val: "https://localhost:8080", Hostname: "localhost:8080", NoSSL: false},
		HostnameFixture{Val: "https://localhost", Error: true},
		HostnameFixture{Val: "localhost", Error: true},
		HostnameFixture{Val: "https://8.8.8.8", Error: true},
		HostnameFixture{Val: "https://internal", Error: true},
		HostnameFixture{Val: "at://pds.example.com", Error: true},
		HostnameFixture{Val: "ftp://pds.example.com", Error: true},
	}

	for _, f := range fixtures {
		hostname, noSSL, err := ParseHostname(f.Val)
		if f.Error {
			assert.Error(err)
			continue
		}
		assert.Equal(f.Hostname, hostname)
		assert.Equal(f.NoSSL, noSSL)
	}
}

type TrustedFixture struct {
	Val     string
	Domains []string
	Trusted bool
}

func TestIsTrustedDomain(t *testing.T) {
	assert := assert.New(t)

	fixtures := []TrustedFixture{
		TrustedFixture{Val: "evil.com", Domains: []string{"good.com"}, Trusted: false},
		TrustedFixture{Val: "pds.host.example.com", Domains: []string{"*.example.com"}, Trusted: true},
		TrustedFixture{Val: "pds.host.example.com", Domains: []string{"example.com"}, Trusted: false},
		TrustedFixture{Val: "pds.host.example.com", Domains: []string{"*.good.com"}, Trusted: false},
		TrustedFixture{Val: "pds.host.example.com", Domains: []string{"*.good.com", "pds.host.example.com"}, Trusted: true},
		TrustedFixture{Val: "good.com", Domains: []string{"*.good.com"}, Trusted: false},
	}

	for _, f := range fixtures {
		assert.Equal(f.Trusted, IsTrustedHostname(f.Val, f.Domains))
	}
}
