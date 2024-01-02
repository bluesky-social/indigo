/*
Package data supports schema-less serializaiton and deserialization of atproto data

Some restrictions from the data model include:
- string sizes
- array and object element counts
- the "shape" of $bytes and $blob data objects
- $type must contain a non-empty string

Details are specified at https://atproto.com/specs/data-model

This package includes types (CIDLink, Bytes, Blob) which are represent the corresponding atproto data model types. These implement JSON and CBOR marshaling in (with whyrusleeping/cbor-gen) the expected way.

Can parse generic atproto records (or other objects) in JSON or CBOR format in to map[string]interface{}, while validating atproto-specific constraints on data (eg, that cid-link objects have only a single field).

Has a helper for serializing generic data (map[string]interface{}) to CBOR, which handles converting JSON-style object types (like $link and $bytes) as needed. There is no "MarshalJSON" method; simply use the standard library's `encoding/json`.
*/
package data
