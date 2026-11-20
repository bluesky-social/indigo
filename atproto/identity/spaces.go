package identity

import (
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Keep recognizable malformed declarations so they cannot become trusted keys
// when an identity is serialized or converted back to a DID document.
func isKEMDeclaration(id string) bool {
	_, fragment, found := strings.Cut(id, "#")
	return (found && fragment == "atproto_kem") || id == "atproto_kem"
}

// KEMPublicKey returns the author's dedicated P-256 #atproto_kem public key.
// The declaration must use its full DID URL, the author's controller DID, and
// the Multikey type. ParseIdentity preserves this declaration for validation.
// Keys alone do not preserve its ID and controller and are not sufficient.
// This method never falls back to the account's signing key.
func (i *Identity) KEMPublicKey() (*atcrypto.PublicKeyP256, error) {
	if _, err := syntax.ParseDID(i.DID.String()); err != nil {
		return nil, fmt.Errorf("invalid KEM identity DID: %w", err)
	}
	if len(i.KEMKeyDeclarations) == 0 {
		return nil, ErrKeyNotDeclared
	}
	if len(i.KEMKeyDeclarations) != 1 {
		return nil, fmt.Errorf("duplicate #atproto_kem key declarations")
	}
	vm := i.KEMKeyDeclarations[0]
	if vm.ID != i.DID.String()+"#atproto_kem" {
		return nil, fmt.Errorf("invalid #atproto_kem key id: %s", vm.ID)
	}
	if vm.Controller != i.DID.String() {
		return nil, fmt.Errorf("#atproto_kem key controller does not match identity DID")
	}
	if vm.Type != "Multikey" {
		return nil, fmt.Errorf("#atproto_kem key must have type Multikey")
	}
	key, err := atcrypto.ParsePublicMultibase(vm.PublicKeyMultibase)
	if err != nil {
		return nil, fmt.Errorf("invalid #atproto_kem key: %w", err)
	}
	p256, ok := key.(*atcrypto.PublicKeyP256)
	if !ok {
		return nil, fmt.Errorf("#atproto_kem key must use P-256")
	}
	return p256, nil
}
