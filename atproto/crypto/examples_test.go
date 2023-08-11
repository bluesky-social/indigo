package crypto

import (
	"encoding/base64"
	"fmt"
)

func ExamplePublicKey() {
	pub, err := ParsePublicDidKey("did:key:zDnaembgSGUhZULN2Caob4HLJPaxBh92N7rtH21TErzqf8HQo")
	if err != nil {
		panic("failed to parse did:key")
	}
	fmt.Println(pub.LegacyDidDocSuite())

	// parse existing base64 message and signature to raw bytes
	msg, _ := base64.RawStdEncoding.DecodeString("oWVoZWxsb2V3b3JsZA")
	sig, _ := base64.RawStdEncoding.DecodeString("2vZNsG3UKvvO/CDlrdvyZRISOFylinBh0Jupc6KcWoJWExHptCfduPleDbG3rko3YZnn9Lw0IjpixVmexJDegg")
	if err = pub.HashAndVerify(msg, sig); err != nil {
		fmt.Println("Verification Failed")
	} else {
		fmt.Println("Success!")
	}
	// Output:
	// EcdsaSecp256r1VerificationKey2019
	// Success!
}

func ExamplePrivateKey() {
	// create secure private key, and corresponding public key
	priv, err := GeneratePrivateKey(K256)
	if err != nil {
		panic("failed to generate key")
	}
	pub := priv.Public()

	// sign a message
	msg := []byte("hello world")
	sig, _ := priv.HashAndSign(msg)

	// verify the message
	if err = pub.HashAndVerify(msg, sig); err != nil {
		fmt.Println("Verification Failed")
	} else {
		fmt.Println("Success!")
	}
	// Output: Success!
}
