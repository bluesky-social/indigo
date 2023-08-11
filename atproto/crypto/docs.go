// Cryptographic keys and operations as used in atproto
//
// This package attempts to abstract away the specific curves, compressions, signature variations, and other implementation details. The goal is to provide as few knobs and options as possible when working with this library. Use of cryptography in atproto is specified in https://atproto.com/specs/cryptography.
//
// The two currently supported curve types are:
//
//   - P-256/secp256r1, internally implemented using golang's stdlib cryptographic library
//   - K-256/secp256r1, internally implemented using <gitlab.com/yawning/secp256k1-voi>
//
// "Low-S" signatures are enforced for both key types, both when creating signatures and during verification, as required by the atproto specification.
//
// This package uses concrete types for private keys, meaning that the secret key material is present in memory.
package crypto
