package oauth

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TODO: attempting to resolve unsafe (SSRF) URLs should error
// TODO: localhost (dev mode) resolution

func TestValidateMetadata(t *testing.T) {
	assert := assert.New(t)

	{
		var meta ProtectedResourceMetadata
		b, err := os.ReadFile("testdata/morel-protected-resource.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
	}

	{
		var meta ProtectedResourceMetadata
		b, err := os.ReadFile("testdata/indie-protected-resource.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
	}

	{
		var meta AuthServerMetadata
		b, err := os.ReadFile("testdata/bsky-entryway-authorization-server.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
		assert.NoError(meta.Validate("https://bsky.social/.well-known/oauth-authorization-server"))
	}

	{
		var meta AuthServerMetadata
		b, err := os.ReadFile("testdata/indie-authorization-server.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
		assert.NoError(meta.Validate("https://pds.robocracy.org/.well-known/oauth-authorization-server"))
	}

	{
		var meta ClientMetadata
		b, err := os.ReadFile("testdata/flaskdemo-client-metadata.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
		assert.NoError(meta.Validate("https://oauth-flask.demo.bsky.dev/oauth/client-metadata.json"))
	}

	{
		var meta ClientMetadata
		b, err := os.ReadFile("testdata/statusphere-client-metadata.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
		assert.NoError(meta.Validate("https://statusphere.mozzius.dev/oauth-client-metadata.json"))
	}

	{
		var meta ClientMetadata
		b, err := os.ReadFile("testdata/smokesignal-client-metadata.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
		assert.NoError(meta.Validate("https://smokesignal.events/oauth/client-metadata.json"))
	}

	{
		var meta JWKS
		b, err := os.ReadFile("testdata/flaskdemo-jwks.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
	}

	{
		var meta JWKS
		b, err := os.ReadFile("testdata/smokesignal-jwks.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			t.Fatal(err)
		}
	}
}
