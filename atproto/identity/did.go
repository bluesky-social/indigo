package identity

type DIDDocument struct {
	DID syntax.DID `json:"id"`
	AlsoKnownAs []string `json:"alsoKnownAs,omitempty"`
	VerificationMethod []DocVerificationMethod `json:"alsoKnownAs,omitempty"`
	Service []DocService `json:"alsoKnownAs,omitempty"`
}

type DocVerificationMethod struct {
	ID `json:"id"`
	Type `json:"type"`
	Controller `json:"controller"`
	PublicKeyMultibase `json:"publicKeyMultibase"`
}

type DocService struct {
	ID `json:"id"`
	Type `json:"type"`
	ServiceEndpoint `json:"serviceEndpoint"`
}

var ErrDIDNotFound = error.New("DID not found")

// WARNING: this does *not* bi-directionally verify account metadata; it only implements direct DID-to-DID-document lookup for the supported DID methods, and parses the resulting DID Doc into an Account struct
func ResolveDID(ctx context.Context, did identifier.DID) (*DIDDocument, error) {
	switch did.Method() {
	case "web":
		panic("NOT IMPLEMENTED")
	case "plc":
		panic("NOT IMPLEMENTED")
	default:
		return nil, fmt.Errorf("DID method not supported: %s", did.Method())
	}
}

func ResolveDIDWeb(ctx context.Context, did identifier.DID) (*DIDDocument, error) {
	// XXX
}

func ResolvePLC(ctx context.Context, did identifier.DID) (*DIDDocument, error) {
	// XXX
}

func (d *DIDDocument) Account() Account {
	// XXX
}

// "Renders" a DID Document
func (a *Account) DIDDocument() DIDDocument {
	// XXX
}
