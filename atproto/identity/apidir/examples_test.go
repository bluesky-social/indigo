package apidir

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

func ExampleAPIDirectory() {
	// don't run this as a CI test!
	//return

	ctx := context.Background()

	// will connect to the provided identity server (eg, a 'domesday' instance)
	dir := NewAPIDirectory("http://localhost:6600")

	handle, _ := syntax.ParseHandle("atproto.com")
	did, _ := syntax.ParseDID("did:plc:ewvi7nxzyoun6zhxrhs64oiz")

	// low-level resolution of a handle (`identity.Resolver` interface)
	atprotoDID, _ := dir.ResolveHandle(ctx, handle)
	fmt.Println(atprotoDID)

	// low-level DID document resolution (`identity.Resolver` interface)
	doc, err := dir.ResolveDID(ctx, did)
	if err != nil {
		panic(err)
	}
	fmt.Println(doc.Service)

	// higher-level identity resolution with accessors (`identity.Directory` interface)
	ident, _ := dir.LookupHandle(ctx, handle)
	fmt.Println(ident.PDSEndpoint())

	/// Output:
	// did:plc:ewvi7nxzyoun6zhxrhs64oiz
	// [{#atproto_pds AtprotoPersonalDataServer https://enoki.us-east.host.bsky.network}]
	// https://enoki.us-east.host.bsky.network
}
