package identity

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// A recognizable but invalid dedicated ID must not turn into an absent entry.
func isSpaceDeclaration(id, fragment string) bool {
	_, frag, found := strings.Cut(id, "#")
	return (found && frag == fragment) || id == fragment
}

func (i *Identity) validSpaceDeclarationID(id, fragment string) bool {
	if _, err := syntax.ParseDID(i.DID.String()); err != nil {
		return false
	}
	return id == "#"+fragment || id == i.DID.String()+"#"+fragment
}

// SpacePublicKey selects #atproto_space, falling back to #atproto only when
// the dedicated declaration is absent. Malformed and duplicate declarations
// are errors, including declarations discarded by the general identity parser.
func (i *Identity) SpacePublicKey() (atcrypto.PublicKey, error) {
	if len(i.SpaceKeyDeclarations) > 0 {
		if len(i.SpaceKeyDeclarations) != 1 {
			return nil, fmt.Errorf("duplicate #atproto_space key declarations")
		}
		vm := i.SpaceKeyDeclarations[0]
		if !i.validSpaceDeclarationID(vm.ID, "atproto_space") {
			return nil, fmt.Errorf("invalid #atproto_space key id: %s", vm.ID)
		}
		if vm.Controller != i.DID.String() {
			return nil, fmt.Errorf("#atproto_space key controller does not match identity DID")
		}
		return parsePublicKey(VerificationMethod{Type: vm.Type, PublicKeyMultibase: vm.PublicKeyMultibase})
	}
	if _, ok := i.Keys["atproto_space"]; ok {
		return i.GetPublicKey("atproto_space")
	}
	return i.PublicKey()
}

// SpaceHostEndpoint selects #atproto_space_host, falling back to #atproto_pds
// only when the dedicated declaration is absent. Dedicated hosts must have
// type AtprotoSpaceHost and an absolute HTTP(S) endpoint. Public PDS endpoint
// parsing is unchanged.
func (i *Identity) SpaceHostEndpoint() (string, error) {
	var endpoint ServiceEndpoint
	if len(i.SpaceHostDeclarations) > 0 {
		if len(i.SpaceHostDeclarations) != 1 {
			return "", fmt.Errorf("duplicate #atproto_space_host service declarations")
		}
		svc := i.SpaceHostDeclarations[0]
		if !i.validSpaceDeclarationID(svc.ID, "atproto_space_host") {
			return "", fmt.Errorf("invalid #atproto_space_host service id: %s", svc.ID)
		}
		endpoint = ServiceEndpoint{Type: svc.Type, URL: svc.ServiceEndpoint}
	} else {
		var ok bool
		endpoint, ok = i.Services["atproto_space_host"]
		if !ok {
			pds := i.PDSEndpoint()
			if pds == "" {
				return "", fmt.Errorf("space host and fallback PDS endpoint not declared or invalid")
			}
			return pds, nil
		}
	}
	if endpoint.Type != "AtprotoSpaceHost" {
		return "", fmt.Errorf("invalid #atproto_space_host service type: %s", endpoint.Type)
	}
	u, err := url.Parse(endpoint.URL)
	if err != nil {
		return "", fmt.Errorf("invalid #atproto_space_host endpoint: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" {
		return "", fmt.Errorf("#atproto_space_host endpoint must be an absolute HTTP(S) URL")
	}
	return endpoint.URL, nil
}
