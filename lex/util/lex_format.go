package util

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"github.com/ipfs/go-cid"
	"github.com/relvacode/iso8601"
	"golang.org/x/xerrors"

	cbg "github.com/whyrusleeping/cbor-gen"
)

// cbor-gen internally uses reflect to determine the type of the field, and
// if the type definition is an alias, it will treat the type as the underlying type(string) instead of the new name.
// For this reason, we need to define a new struct for each format types, not using an alias.

type FormatHandle struct{ str string }

func NewFormatHandle(s string) FormatHandle {
	return FormatHandle{s}
}

// Handle is a subset of AtIdentifier.
func (f FormatHandle) AsAtIdentifier() FormatAtIdentifier {
	return FormatAtIdentifier{f.str}
}

// https://github.com/bluesky-social/atproto/blob/main/packages/identifier/src/handle.ts
var regexValidHandle = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func ParseFormatHandle(s string) (FormatHandle, error) {
	if !regexValidHandle.MatchString(s) {
		return FormatHandle{""}, fmt.Errorf("invalid handle: %s", s)
	}

	if len(s) > 253 {
		return FormatHandle{""}, fmt.Errorf("handle is too long(253 chars max): %s", s)
	}

	return FormatHandle{s}, nil
}

func (f FormatHandle) String() string {
	return f.str
}

func (f FormatHandle) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write handle string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatHandle) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read handle string from CBOR: %w", err)
	}
	fp, err := ParseFormatHandle(s)
	if err != nil {
		return xerrors.Errorf("failed to parse handle string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatHandle) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatHandle) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal handle string: %w", err)
	}
	fp, err := ParseFormatHandle(s)
	if err != nil {
		return xerrors.Errorf("failed to parse handle string: %w", err)
	}

	*f = fp

	return nil
}

type FormatNSID struct{ str string }

func NewFormatNSID(s string) FormatNSID {
	return FormatNSID{s}
}

// https://github.com/bluesky-social/atproto/blob/main/packages/nsid/src/index.ts
var regexValidNSID = regexp.MustCompile(`^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+(\.[a-zA-Z]([a-zA-Z0-9-]{0,126}[a-zA-Z0-9])?)$`)

func ParseFormatNSID(s string) (FormatNSID, error) {
	if !regexValidNSID.MatchString(s) {
		return FormatNSID{""}, fmt.Errorf("invalid NSID: %s", s)
	}

	if len(s) > 253+1+128 {
		return FormatNSID{""}, fmt.Errorf("NSID is too long(382 chars max): %s", s)
	}

	return FormatNSID{s}, nil
}

func (f FormatNSID) String() string {
	return f.str
}

func (f FormatNSID) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write NSID string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatNSID) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read NSID string from CBOR: %w", err)
	}
	fp, err := ParseFormatNSID(s)
	if err != nil {
		return xerrors.Errorf("failed to parse NSID string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatNSID) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatNSID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal NSID string: %w", err)
	}
	fp, err := ParseFormatNSID(s)
	if err != nil {
		return xerrors.Errorf("failed to parse NSID string: %w", err)
	}

	*f = fp

	return nil
}

type FormatAtIdentifier struct{ str string }

func NewFormatAtIdentifier(s string) FormatAtIdentifier {
	return FormatAtIdentifier{s}
}

func ParseFormatAtIdentifier(s string) (FormatAtIdentifier, error) {
	if regexValidDID.MatchString(s) {
		return FormatAtIdentifier{s}, nil
	}

	if regexValidHandle.MatchString(s) {
		return FormatAtIdentifier{s}, nil
	}

	return FormatAtIdentifier{""}, fmt.Errorf("invalid at-identifier: %s", s)
}

func (f FormatAtIdentifier) String() string {
	return f.str
}

func (f FormatAtIdentifier) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write at-identifier string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatAtIdentifier) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read at-identifier string from CBOR: %w", err)
	}
	fp, err := ParseFormatAtIdentifier(s)
	if err != nil {
		return xerrors.Errorf("failed to parse at-identifier string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatAtIdentifier) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatAtIdentifier) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal at-identifier string: %w", err)
	}
	fp, err := ParseFormatAtIdentifier(s)
	if err != nil {
		return xerrors.Errorf("failed to parse at-identifier string: %w", err)
	}

	*f = fp

	return nil
}

type FormatURI struct{ str string }

var regexValidURI = regexp.MustCompile(`^\w+:(?:\/\/)?[^\s/][^\s]*$`)

func ParseFormatURI(s string) (FormatURI, error) {
	if !regexValidURI.MatchString(s) {
		return FormatURI{""}, fmt.Errorf("invalid URI: %s", s)
	}

	return FormatURI{s}, nil
}

func (f FormatURI) String() string {
	return f.str
}

func (f FormatURI) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write URI string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatURI) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read URI string from CBOR: %w", err)
	}
	fp, err := ParseFormatURI(s)
	if err != nil {
		return xerrors.Errorf("failed to parse URI string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatURI) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatURI) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal URI string: %w", err)
	}
	fp, err := ParseFormatURI(s)
	if err != nil {
		return xerrors.Errorf("failed to parse URI string: %w", err)
	}

	*f = fp

	return nil
}

type FormatDateTime struct{ str string }

func ParseDateTime(s string) (FormatDateTime, error) {
	if _, err := iso8601.ParseString(s); err != nil {
		return FormatDateTime{""}, fmt.Errorf("invalid datetime: %s", s)
	}

	return FormatDateTime{s}, nil
}

func (f FormatDateTime) String() string {
	return f.str
}

func (f FormatDateTime) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write datetime string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatDateTime) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read datetime string from CBOR: %w", err)
	}
	fp, err := ParseDateTime(s)
	if err != nil {
		return xerrors.Errorf("failed to parse datetime string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatDateTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatDateTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal datetime string: %w", err)
	}
	fp, err := ParseDateTime(s)
	if err != nil {
		return xerrors.Errorf("failed to parse datetime string: %w", err)
	}

	*f = fp

	return nil
}

type FormatAtURI struct{ str string }

func NewFormatAtURI(s string) FormatAtURI {
	return FormatAtURI{s}
}

// https://github.com/bluesky-social/atproto/blob/main/packages/uri/src/validation.ts
var regexValidAtURI = regexp.MustCompile(`^at:\/\/(?P<authority>[a-zA-Z0-9._:%-]+)(\/(?P<collection>[a-zA-Z0-9-.]+)(\/(?P<rkey>[a-zA-Z0-9._~:@!$&%')(*+,;=-]+))?)?(#(?P<fragment>\/[a-zA-Z0-9._~:@!$&%')(*+,;=\-[\]/\\]*))?$`)
var authorityIndex = regexValidAtURI.SubexpIndex("authority")
var collectionIndex = regexValidAtURI.SubexpIndex("collection")
var rkeyIndex = regexValidAtURI.SubexpIndex("rkey")
var fragmentIndex = regexValidAtURI.SubexpIndex("fragment")

func ParseAtURI(s string) (FormatAtURI, error) {
	re := regexValidAtURI.FindStringSubmatch(s)
	if re == nil { // indicates regex matching failed
		return FormatAtURI{""}, fmt.Errorf("invalid at-uri: %s", s)
	}

	authority := re[authorityIndex]
	if !regexValidHandle.MatchString(authority) {
		if !regexValidDID.MatchString(authority) {
			return FormatAtURI{""}, fmt.Errorf("invalid at-uri authority: %s", authority)
		}
	}

	collection := re[collectionIndex]
	if collection != "" && !regexValidNSID.MatchString(collection) {
		return FormatAtURI{""}, fmt.Errorf("invalid at-uri collection: %s", collection)
	}

	if len(s) > 8*1024 {
		return FormatAtURI{""}, fmt.Errorf("at-uri too long: %s", s)
	}

	return FormatAtURI{s}, nil
}

func (f FormatAtURI) String() string {
	return f.str
}

func (f FormatAtURI) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write at-uri string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatAtURI) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read at-uri string from CBOR: %w", err)
	}
	fp, err := ParseAtURI(s)
	if err != nil {
		return xerrors.Errorf("failed to parse at-uri string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatAtURI) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatAtURI) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal at-uri string: %w", err)
	}
	fp, err := ParseAtURI(s)
	if err != nil {
		return xerrors.Errorf("failed to parse at-uri string: %w", err)
	}

	*f = fp

	return nil
}

type FormatDID struct{ str string }

func NewFormatDID(s string) FormatDID {
	return FormatDID{s}
}

// DID is a subset of AtIdentifier.
func (f FormatDID) AsAtIdentifier() FormatAtIdentifier {
	return FormatAtIdentifier{f.str}
}

// https://github.com/bluesky-social/atproto/blob/main/packages/identifier/src/did.ts
var regexValidDID = regexp.MustCompile(`^did:[a-z]+:[a-zA-Z0-9._:%-]*[a-zA-Z0-9._-]$`)

func ParseFormatDID(s string) (FormatDID, error) {
	if regexValidDID.MatchString(s) {
		return FormatDID{s}, nil
	}
	return FormatDID{""}, fmt.Errorf("invalid DID: %s", s)
}

func (f FormatDID) String() string {
	return f.str
}

func (f FormatDID) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write DID string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatDID) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read DID string from CBOR: %w", err)
	}
	fp, err := ParseFormatDID(s)
	if err != nil {
		return xerrors.Errorf("failed to parse DID string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatDID) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatDID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal DID string: %w", err)
	}
	fp, err := ParseFormatDID(s)
	if err != nil {
		return xerrors.Errorf("failed to parse DID string: %w", err)
	}

	*f = fp

	return nil
}

type FormatCID struct{ str string }

func NewFormatCID(s string) FormatCID {
	return FormatCID{s}
}

func ParseFormatCID(s string) (FormatCID, error) {
	if _, err := cid.Decode(s); err == nil {
		return FormatCID{s}, nil
	}
	return FormatCID{""}, fmt.Errorf("invalid CID: %s", s)
}

func (f FormatCID) String() string {
	return f.str
}

func (f FormatCID) MarshalCBOR(w io.Writer) error {
	cw := cbg.NewCborWriter(w)
	err := MarshalFormatString(cw, f.str)
	if err != nil {
		return xerrors.Errorf("failed to write CID string as CBOR: %w", err)
	}

	return nil
}

func (f *FormatCID) UnmarshalCBOR(r io.Reader) error {
	cr := cbg.NewCborReader(r)
	s, err := cbg.ReadString(cr)
	if err != nil {
		return xerrors.Errorf("failed to read CID string from CBOR: %w", err)
	}
	fp, err := ParseFormatCID(s)
	if err != nil {
		return xerrors.Errorf("failed to parse CID string: %w", err)
	}

	*f = fp

	return nil
}

func (f FormatCID) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.str)
}

func (f *FormatCID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return xerrors.Errorf("failed to unmarshal CID string: %w", err)
	}
	fp, err := ParseFormatCID(s)
	if err != nil {
		return xerrors.Errorf("failed to parse CID string: %w", err)
	}

	*f = fp

	return nil
}

///////////////////////
// Marshal helper

func MarshalFormatString(cw *cbg.CborWriter, f string) error {
	if len(f) == 0 {
		_, err := cw.Write(cbg.CborNull)
		return err
	}

	if len(f) > cbg.MaxLength {
		return xerrors.Errorf("Value in field format was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(f))); err != nil {
		return err
	}
	if _, err := io.WriteString(cw, f); err != nil {
		return err
	}

	return nil
}
