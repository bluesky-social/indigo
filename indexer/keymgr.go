package indexer

import (
	"context"
	"fmt"
	"log/slog"

	did "github.com/whyrusleeping/go-did"
	"go.opentelemetry.io/otel"
)

type KeyManager struct {
	didr DidResolver

	signingKey *did.PrivKey

	log *slog.Logger
}

type DidResolver interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
}

func NewKeyManager(didr DidResolver, k *did.PrivKey) *KeyManager {
	return &KeyManager{
		didr:       didr,
		signingKey: k,
		log:        slog.Default().With("system", "indexer"),
	}
}

func (km *KeyManager) VerifyUserSignature(ctx context.Context, did string, sig []byte, msg []byte) error {
	ctx, span := otel.Tracer("keymgr").Start(ctx, "verifySignature")
	defer span.End()

	k, err := km.getKey(ctx, did)
	if err != nil {
		return err
	}

	err = k.Verify(msg, sig)
	if err != nil {
		km.log.Warn("signature failed to verify", "err", err, "did", did, "pubKey", k, "sigBytes", sig, "msgBytes", msg)
	}
	return err
}

func (km *KeyManager) getKey(ctx context.Context, did string) (*did.PubKey, error) {
	ctx, span := otel.Tracer("keymgr").Start(ctx, "getKey")
	defer span.End()

	// TODO: caching should be done at the DID document level, that way we can
	// have a thing that subscribes to plc updates for cache busting
	doc, err := km.didr.GetDocument(ctx, did)
	if err != nil {
		return nil, err
	}

	pubk, err := doc.GetPublicKey("#atproto")
	if err != nil {
		return nil, err
	}

	return pubk, nil
}

func (km *KeyManager) SignForUser(ctx context.Context, did string, msg []byte) ([]byte, error) {
	if km.signingKey == nil {
		return nil, fmt.Errorf("key manager does not have a signing key, cannot sign")
	}

	return km.signingKey.Sign(msg)
}
