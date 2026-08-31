package ssrf

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPublicOnlyTransport(t *testing.T) {
	assert := assert.New(t)

	c := http.Client{
		Transport: PublicOnlyTransport(),
		Timeout:   1 * time.Millisecond,
	}

	{
		_, err := c.Get("http://127.0.0.1:2470/")
		assert.ErrorIs(err, ErrUnsafeNetworkAddress)
	}

	{
		_, err := c.Get("http://[::1]/")
		assert.ErrorIs(err, ErrUnsafeNetworkAddress)
	}

	{
		_, err := c.Get("http://[64:ff9b::c0a8:0101]/") // NAT64 encoding of 192.168.1.1
		assert.ErrorIs(err, ErrUnsafeNetworkAddress)
	}

	{
		_, err := c.Get("http://[fd00:ec2::254]/") // IPv6 ULA (eg, AWS IMDS)
		assert.ErrorIs(err, ErrUnsafeNetworkAddress)
	}

	{
		_, err := c.Get("http://[::ffff:169.254.169.254]/") // another IPv4-mapped IPv6 address
		assert.ErrorIs(err, ErrUnsafeNetworkAddress)
	}

	{
		_, err := c.Get("http://localhost:2470/path")
		assert.ErrorIs(err, ErrUnsafeNetworkAddress)
	}

	{
		_, err := c.Get("http://8.8.8.8:8080/path") // disallowed port number
		assert.ErrorIs(err, ErrUnsafeNetworkAddress)
	}
}
