package event

// Base type for events specific to an account, usually derived from a repo event stream message (one such message may result in multiple `RepoEvent`)
//
// Events are both containers for data about the event itself (similar to an HTTP request type); aggregate results and state (counters, mod actions) to be persisted after all rules are run; and act as an API for additional network reads and operations.
//
// Handling of moderation actions (such as labels, flags, and reports) are deferred until the end of all rule execution, then de-duplicated against any pre-existing actions on the account.
type RepoEvent struct {
	// Metadata for the account (identity) associated with this event (aka, the repo owner)
	Account AccountMeta
}

// Alias of RepoEvent
type IdentityEvent struct {
	RepoEvent
}

// Extends RepoEvent. Represents the creation of a single record in the given repository.
type RecordEvent struct {
	RepoEvent

	// The un-marshalled record, as a go struct, from the api/atproto or api/bsky type packages.
	Record any
	// The "collection" part of the repo path for this record. Must be an NSID, though this isn't indicated by the type of this field.
	Collection string
	// The "record key" (rkey) part of repo path.
	RecordKey string
	// CID of the canonical CBOR version of the record, as matches the repo value.
	CID string
}

// Extends RepoEvent. Represents the deletion of a single record in the given repository.
type RecordDeleteEvent struct {
	RepoEvent

	Collection string
	RecordKey  string
}
