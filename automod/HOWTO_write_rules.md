
HOWTO: Write automod Rules
==========================

The short version is:

- identity a behavior pattern or type of content in the network, and an action that should be taken in response
- write a "rule" function, in golang, which will detect this pattern (usually start by copying an existing rule)
- register the new rule with the rule engine
- test triggering the rule, either in a test network or using "captured" content from a real network
- deploy the rule, first with reduced "effects" (actions) to monitor impact

The `automod/rules` package contains a set of example rules and some shared helper functions, and demonstrates some patterns for how to use counters, sets, filters, and account metadata to compose a rule pattern.

## How Rules Work

Automod rules are golang functions which get called every time a relevant event takes place in the network. Rule functions receive static metadata about the event; can fetch additional state or metadata as needed; and can optionally output "effects". These effects can include state mutations (such as incrementing counters), or taking moderation actions.

There are multiple rule function types (eg, specifically for bsky "posts", or for atproto identity updates), but they all receive a `c` "Context" argument as the primary API for the rules system, including both accessing metadata and recording effects.

Multiple rules for the same event may be run concurrently, or in arbitrary order. Effects *are not* visible between rule execution on the same event, and are only persisted after all rules have finished executing. This means that if one rule increments a counter or adds a label, other rules will not "see" that effect when processing the same event.

Effects are automatically de-duplicated by the rules engine, both between concurrent rules and against the current state of an effect's subject. This means that rules can generally "trigger" continuously (eg, report an account on the basis of multiple posts), and the action will only take place once (not reported multiple times).

It is expected that some rules will act together, for example paired rules on record creation and record deletion.

The design philosophy of rules are that they mostly contain their own configuration, as code. Rules are not expected to be directly configurable, and changing the "effects" or action of a rule is a change to the rule code itself.


## Rule APIs

There are two general categories of rules and effects: at the account-level, and at the record-level, with the later being a superset of the former.

Note that none of the Context methods return errors. If errors are encountered (for example, network faults), error state is persisted internally to the Context object, a placeholder value is returned, and no effects will be persisted for the overall event execution. This is to keep rule code simple and readable.


### Rule Types

The notable rule function types are:

- `type IdentityRuleFunc = func(c *AccountContext) error`: triggers on events like handle updates or account migrations
- `type RecordRuleFunc = func(c *RecordContext) error`: triggers on every repo operation: create, update, or delete. Triggers for every record type, including posts and profiles
- `type PostRuleFunc = func(c *RecordContext, post *appbsky.FeedPost) error`: triggers on creation or update of any `app.bsky.feed.post` record. The post record is de-serialized for convenience, but otherwise this is basically just `RecordRuleFunc`
- `type ProfileRuleFunc = func(c *RecordContext, profile *appbsky.ActorProfile) error`: same as `PostRuleFunc`, but for profile

The `PostRuleFunc` and `ProfileRuleFunc` are simply affordances so that rules for those common record types don't all need to filter and type-cast. Rules for other record types (such as `app.bsky.graph.follow`) do need to use `RecordRuleFunc` and implement that filtering and type-casting.

### Pre-Hydrated Metadata

The `c *automod.AccountContext` parameter provides the following pre-hydrated metadata:

- `c.Account.Identity`: atproto identity for the account, including `DID` and `Handle` fields, and the PDS endpoint URL (if declared)
- `c.Account.Private` (optional): contains things like `.IndexedAt` (account first seen), `.Email` (the current registered account email), and `.EmailConfirmed` (boolean). Only hydrated when the rule engine is configured with admin privileges, and the account is on a PDS those privileges have access to
- `c.Account.Profile` (optional): a cached subset of the account's bsky profile record
- `c.Account.AccountLabels` (array of strings): cached view of any moderation labels applied to the account, by the relevant "local" moderation service
- `c.Account.AccountNegatedLabels` (array of strings)
- `c.Account.Takendown` (bool): if the account is currently taken down or not
- `c.Account.FollowersCount` (int64): cached
- `c.Account.PostsCount` (int64): cached

The `c *automod.RecordContext` parameter is a superset of `AccountContext` and also includes:

- `c.RecordOp.Action`: one of "create", "update", or "delete"
- `c.RecordOp.DID`
- `c.RecordOp.Collection`
- `c.RecordOp.RecordKey`
- `c.RecordOp.CID` (optional): not included for "delete"
- `c.RecordOp.Value` (optional): the record itself, usually as a pointer to an un-marshalled struct

### Counters

All `Context` objects provide access to counters. Rules don't need to pre-configure counter namespaces or values, they can just start using them. The default value for a counter which has never been incremented is `0`.

The datastore providing counters is an internal implementation/configuration detail of the rule engine, but is usually Redis. Reads (`GetCount`) may hit the network but are pretty fast.

Incrementing a counter is an "effect" and is not persisted until the end of all rule execution for an event. That is, if you read, increment, and read again, you will read the same count.

The counter API has distinct "namespace" and "value" fields, which are combined to form a key. You generally chose a unique namespace specific to your rule and counter type, and then values are either a fixed string or a normalized field like a DID or hash. The keyspace is global, so rules can access and mutate each other's counters, and need to avoid namespace collisions.

Time periods for counters:

- `automod.PeriodHour`: time bucket of current hour
- `automod.PeriodDay`: time bucket of current day
- `automod.PeriodTotal`: all-time counts

Basic counters:

- `c.GetCount(<namespace-string>, <value-string>, <time-period>)`: reads count for the specific time period
- `c.Increment(<namespace-string>, <value-string>)`: increments all time periods
- `c.IncrementPeriod(<namespace-string>, <value-string>, <time-period>)`: increments only a single time period bucket, as a resource optimization. You should generally use the full `Increment` method.

"Distinct value" counters use a statistical data structure (hyperloglog) to estimate the number of unique strings incremented for the given bucket. These counters consume more memory (up to a couple KBytes per counter), though they are generally smaller for small-N buckets.

- `c.GetCountDistinct(<namespace>, <bucket>, <time-period>)`
- `c.IncrementDistinct(<namespace>, <bucket>, <value>)`

### Sets

Sets are a mechanism to separate configuration from rule implementation. They are simply named arrays of strings. Membership checks are very fast, and won't hit the network more than once per set per rule invocation.

- `c.InSet(<set-name>, <value>)`: checks if a string is in a named set, returning a `bool`

### Moderation Effects (Actions)

"Flags" are a concept invented for automod. They are essentially private labels: string values attached to a subject (account or record) and persisted.

Rules can take account-level actions using the following methods:

- `c.AddAccountFlag(val string)`
- `c.AddAccountLabel(val string)`
- `c.ReportAccount(reason string, comment string)`
- `c.TakedownAccount()`

The `RecordContext` additionally has record-level equivalents for all these methods.

### Other Stuff

- `c.Logger`: a `log/slog` logging interface. Logging currently happens immediately, instead of being accumulated as an "effect"
- `c.Directory()`: returns an `identity.Directory` (interface), which can be used for (cached) identity resolution

## Development Process

When deploying a new rule, it is recommended to start with a minimal action, like setting a flag or just logging. Any "action" (including new flag creation) can result in a Slack notification. You can gain confidence in the rule by running against the full firehose with these limited actions, tweaking the rule until it seems to have acceptable sensitivity (eg, few false positives), and then escalate the actions to reporting (adds to the human review queue), or action-and-report (label or takedown, and concurrently report for humans to review the action).

### Network Data

The `hepa` command provides `process-record` and `process-recent` sub-commands which will pull an existing individual record (by AT-URI) or all recent bsky posts for an account (by handle or DID), which can be helpful for testing.

There is also a `capture-recent` sub-command which will save a snapshot ("capture") of the current account identity and profile, and recent bsky posts, as JSON. This can be combined with testing helpers (which will load the capture and push it through a mock rules engine) to test that new rules actually trigger as expected against real-world data.

Note that, of course, any real-world captures should have identifying or otherwise sensitive information redacted or replaced before committing to git.


## Examples

Here is a trivial post record rule:

```golang
// the GTUBE string is a special value historically used to test email spam filtering behavior
var gtubeString = "XJS*C4JDBQADN1.NSBN3*2IDNEN*GTUBE-STANDARD-ANTI-UBE-TEST-EMAIL*C.34X"

func GtubePostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if strings.Contains(post.Text, gtubeString) {
		c.AddRecordLabel("spam")
	}
	return nil
}
```

Every new (or updated) post is checked for an exact string match, and is labeled "spam" if found.
