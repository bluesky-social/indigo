package util

import "context"

type FakeKeyManager struct {
}

func (km *FakeKeyManager) VerifyUserSignature(context.Context, string, []byte, []byte) error {
	return nil
}

func (km *FakeKeyManager) SignForUser(ctx context.Context, did string, msg []byte) ([]byte, error) {
	return []byte("signature"), nil
}
