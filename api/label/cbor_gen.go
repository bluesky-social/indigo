// Code generated by github.com/whyrusleeping/cbor-gen. DO NOT EDIT.

package label

import (
	"fmt"
	"io"
	"math"
	"sort"

	cid "github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	xerrors "golang.org/x/xerrors"
)

var _ = xerrors.Errorf
var _ = cid.Undef
var _ = math.E
var _ = sort.Sort

func (t *Label) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 7

	if t.Cid == nil {
		fieldCount--
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// t.Cid (string) (string)
	if t.Cid != nil {

		if len("cid") > cbg.MaxLength {
			return xerrors.Errorf("Value in field \"cid\" was too long")
		}

		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("cid"))); err != nil {
			return err
		}
		if _, err := io.WriteString(w, string("cid")); err != nil {
			return err
		}

		if t.Cid == nil {
			if _, err := cw.Write(cbg.CborNull); err != nil {
				return err
			}
		} else {
			if len(*t.Cid) > cbg.MaxLength {
				return xerrors.Errorf("Value in field t.Cid was too long")
			}

			if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(*t.Cid))); err != nil {
				return err
			}
			if _, err := io.WriteString(w, string(*t.Cid)); err != nil {
				return err
			}
		}
	}

	// t.Cts (string) (string)
	if len("cts") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"cts\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("cts"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("cts")); err != nil {
		return err
	}

	if len(t.Cts) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.Cts was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Cts))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.Cts)); err != nil {
		return err
	}

	// t.Neg (bool) (bool)
	if len("neg") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"neg\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("neg"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("neg")); err != nil {
		return err
	}

	if err := cbg.WriteBool(w, t.Neg); err != nil {
		return err
	}

	// t.Src (string) (string)
	if len("src") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"src\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("src"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("src")); err != nil {
		return err
	}

	if len(t.Src) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.Src was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Src))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.Src)); err != nil {
		return err
	}

	// t.Uri (string) (string)
	if len("uri") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"uri\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("uri"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("uri")); err != nil {
		return err
	}

	if len(t.Uri) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.Uri was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Uri))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.Uri)); err != nil {
		return err
	}

	// t.Val (string) (string)
	if len("val") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"val\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("val"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("val")); err != nil {
		return err
	}

	if len(t.Val) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.Val was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Val))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.Val)); err != nil {
		return err
	}

	// t.LexiconTypeID (string) (string)
	if len("$type") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"$type\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("$type")); err != nil {
		return err
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("com.atproto.label.label"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("com.atproto.label.label")); err != nil {
		return err
	}
	return nil
}

func (t *Label) UnmarshalCBOR(r io.Reader) (err error) {
	*t = Label{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("Label: map struct too large (%d)", extra)
	}

	var name string
	n := extra

	for i := uint64(0); i < n; i++ {

		{
			sval, err := cbg.ReadString(cr)
			if err != nil {
				return err
			}

			name = string(sval)
		}

		switch name {
		// t.Cid (string) (string)
		case "cid":

			{
				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}

					sval, err := cbg.ReadString(cr)
					if err != nil {
						return err
					}

					t.Cid = (*string)(&sval)
				}
			}
			// t.Cts (string) (string)
		case "cts":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.Cts = string(sval)
			}
			// t.Neg (bool) (bool)
		case "neg":

			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}
			if maj != cbg.MajOther {
				return fmt.Errorf("booleans must be major type 7")
			}
			switch extra {
			case 20:
				t.Neg = false
			case 21:
				t.Neg = true
			default:
				return fmt.Errorf("booleans are either major type 7, value 20 or 21 (got %d)", extra)
			}
			// t.Src (string) (string)
		case "src":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.Src = string(sval)
			}
			// t.Uri (string) (string)
		case "uri":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.Uri = string(sval)
			}
			// t.Val (string) (string)
		case "val":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.Val = string(sval)
			}
			// t.LexiconTypeID (string) (string)
		case "$type":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.LexiconTypeID = string(sval)
			}

		default:
			// Field doesn't exist on this type, so ignore it
			cbg.ScanForLinks(r, func(cid.Cid) {})
		}
	}

	return nil
}
func (t *SubscribeLabels_Info) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 3

	if t.LexiconTypeID == "" {
		fieldCount--
	}

	if t.Message == nil {
		fieldCount--
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// t.Name (string) (string)
	if len("name") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"name\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("name"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("name")); err != nil {
		return err
	}

	if len(t.Name) > cbg.MaxLength {
		return xerrors.Errorf("Value in field t.Name was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.Name))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string(t.Name)); err != nil {
		return err
	}

	// t.LexiconTypeID (string) (string)
	if t.LexiconTypeID != "" {

		if len("$type") > cbg.MaxLength {
			return xerrors.Errorf("Value in field \"$type\" was too long")
		}

		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
			return err
		}
		if _, err := io.WriteString(w, string("$type")); err != nil {
			return err
		}

		if len(t.LexiconTypeID) > cbg.MaxLength {
			return xerrors.Errorf("Value in field t.LexiconTypeID was too long")
		}

		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.LexiconTypeID))); err != nil {
			return err
		}
		if _, err := io.WriteString(w, string(t.LexiconTypeID)); err != nil {
			return err
		}
	}

	// t.Message (string) (string)
	if t.Message != nil {

		if len("message") > cbg.MaxLength {
			return xerrors.Errorf("Value in field \"message\" was too long")
		}

		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("message"))); err != nil {
			return err
		}
		if _, err := io.WriteString(w, string("message")); err != nil {
			return err
		}

		if t.Message == nil {
			if _, err := cw.Write(cbg.CborNull); err != nil {
				return err
			}
		} else {
			if len(*t.Message) > cbg.MaxLength {
				return xerrors.Errorf("Value in field t.Message was too long")
			}

			if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(*t.Message))); err != nil {
				return err
			}
			if _, err := io.WriteString(w, string(*t.Message)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *SubscribeLabels_Info) UnmarshalCBOR(r io.Reader) (err error) {
	*t = SubscribeLabels_Info{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("SubscribeLabels_Info: map struct too large (%d)", extra)
	}

	var name string
	n := extra

	for i := uint64(0); i < n; i++ {

		{
			sval, err := cbg.ReadString(cr)
			if err != nil {
				return err
			}

			name = string(sval)
		}

		switch name {
		// t.Name (string) (string)
		case "name":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.Name = string(sval)
			}
			// t.LexiconTypeID (string) (string)
		case "$type":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.LexiconTypeID = string(sval)
			}
			// t.Message (string) (string)
		case "message":

			{
				b, err := cr.ReadByte()
				if err != nil {
					return err
				}
				if b != cbg.CborNull[0] {
					if err := cr.UnreadByte(); err != nil {
						return err
					}

					sval, err := cbg.ReadString(cr)
					if err != nil {
						return err
					}

					t.Message = (*string)(&sval)
				}
			}

		default:
			// Field doesn't exist on this type, so ignore it
			cbg.ScanForLinks(r, func(cid.Cid) {})
		}
	}

	return nil
}
func (t *SubscribeLabels_Labels) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}

	cw := cbg.NewCborWriter(w)
	fieldCount := 3

	if t.LexiconTypeID == "" {
		fieldCount--
	}

	if _, err := cw.Write(cbg.CborEncodeMajorType(cbg.MajMap, uint64(fieldCount))); err != nil {
		return err
	}

	// t.Seq (int64) (int64)
	if len("seq") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"seq\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("seq"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("seq")); err != nil {
		return err
	}

	if t.Seq >= 0 {
		if err := cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(t.Seq)); err != nil {
			return err
		}
	} else {
		if err := cw.WriteMajorTypeHeader(cbg.MajNegativeInt, uint64(-t.Seq-1)); err != nil {
			return err
		}
	}

	// t.LexiconTypeID (string) (string)
	if t.LexiconTypeID != "" {

		if len("$type") > cbg.MaxLength {
			return xerrors.Errorf("Value in field \"$type\" was too long")
		}

		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("$type"))); err != nil {
			return err
		}
		if _, err := io.WriteString(w, string("$type")); err != nil {
			return err
		}

		if len(t.LexiconTypeID) > cbg.MaxLength {
			return xerrors.Errorf("Value in field t.LexiconTypeID was too long")
		}

		if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(t.LexiconTypeID))); err != nil {
			return err
		}
		if _, err := io.WriteString(w, string(t.LexiconTypeID)); err != nil {
			return err
		}
	}

	// t.Labels ([]*label.Label) (slice)
	if len("labels") > cbg.MaxLength {
		return xerrors.Errorf("Value in field \"labels\" was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajTextString, uint64(len("labels"))); err != nil {
		return err
	}
	if _, err := io.WriteString(w, string("labels")); err != nil {
		return err
	}

	if len(t.Labels) > cbg.MaxLength {
		return xerrors.Errorf("Slice value in field t.Labels was too long")
	}

	if err := cw.WriteMajorTypeHeader(cbg.MajArray, uint64(len(t.Labels))); err != nil {
		return err
	}
	for _, v := range t.Labels {
		if err := v.MarshalCBOR(cw); err != nil {
			return err
		}
	}
	return nil
}

func (t *SubscribeLabels_Labels) UnmarshalCBOR(r io.Reader) (err error) {
	*t = SubscribeLabels_Labels{}

	cr := cbg.NewCborReader(r)

	maj, extra, err := cr.ReadHeader()
	if err != nil {
		return err
	}
	defer func() {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
	}()

	if maj != cbg.MajMap {
		return fmt.Errorf("cbor input should be of type map")
	}

	if extra > cbg.MaxLength {
		return fmt.Errorf("SubscribeLabels_Labels: map struct too large (%d)", extra)
	}

	var name string
	n := extra

	for i := uint64(0); i < n; i++ {

		{
			sval, err := cbg.ReadString(cr)
			if err != nil {
				return err
			}

			name = string(sval)
		}

		switch name {
		// t.Seq (int64) (int64)
		case "seq":
			{
				maj, extra, err := cr.ReadHeader()
				var extraI int64
				if err != nil {
					return err
				}
				switch maj {
				case cbg.MajUnsignedInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 positive overflow")
					}
				case cbg.MajNegativeInt:
					extraI = int64(extra)
					if extraI < 0 {
						return fmt.Errorf("int64 negative overflow")
					}
					extraI = -1 - extraI
				default:
					return fmt.Errorf("wrong type for int64 field: %d", maj)
				}

				t.Seq = int64(extraI)
			}
			// t.LexiconTypeID (string) (string)
		case "$type":

			{
				sval, err := cbg.ReadString(cr)
				if err != nil {
					return err
				}

				t.LexiconTypeID = string(sval)
			}
			// t.Labels ([]*label.Label) (slice)
		case "labels":

			maj, extra, err = cr.ReadHeader()
			if err != nil {
				return err
			}

			if extra > cbg.MaxLength {
				return fmt.Errorf("t.Labels: array too large (%d)", extra)
			}

			if maj != cbg.MajArray {
				return fmt.Errorf("expected cbor array")
			}

			if extra > 0 {
				t.Labels = make([]*Label, extra)
			}

			for i := 0; i < int(extra); i++ {

				var v Label
				if err := v.UnmarshalCBOR(cr); err != nil {
					return err
				}

				t.Labels[i] = &v
			}

		default:
			// Field doesn't exist on this type, so ignore it
			cbg.ScanForLinks(r, func(cid.Cid) {})
		}
	}

	return nil
}
