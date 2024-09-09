`goat`: Go AT protocol CLI tool
===============================

This is a re-implementation of [adenosine-cli](https://gitlab.com/bnewbold/adenosine/-/tree/main/adenosine-cli?ref_type=heads) in golang.


## Install

If you have the Go toolchain installed and configured correctly, you can directly build and install the tool for your local account:

```bash
go install github.com/bluesky-social/indigo/cmd/goat@latest
```

A more manual way to install is:

```bash
git clone https://github.com/bluesky-social/indigo
go build ./cmd/goat
sudo cp goat /usr/local/bin
```

The intention is to also provide a Homebrew "cask" and Debian/Ubuntu packages.


## Usage

`goat` is relatively self-documenting via help pages:

```bash
goat --help
goat bsky -h
goat help bsky
# etc
```

Most commands use public APIs are don't require authentication. Some commands, like creating records, require an atproto account. You can log in using an "app password" with `goat account login -u <handle> -p <app-password>`.

WARNING: `goat` will store both the app password and authentication tokens in the current users home directory, in cleartext. `goat logout` will wipe the file. Intention is to eventually support configuration via environment variables to keep sensitive state in a password manager or otherwise not-cleartext-on-disk.

Some commands output JSON, and you can use tools like `jq` to process them.

## Examples

Resolve an account's identity in the network:

```bash
$ goat resolve wyden.senate.gov
{
  "id": "did:plc:ydtsvzzsl6nlfkmnuooeqcmc",
  "alsoKnownAs": [
    "at://wyden.senate.gov"
  ],
  "verificationMethod": [
    {
      "id": "did:plc:ydtsvzzsl6nlfkmnuooeqcmc#atproto",
      "type": "Multikey",
      "controller": "did:plc:ydtsvzzsl6nlfkmnuooeqcmc",
      "publicKeyMultibase": "zQ3shuMW7q4KBdsFcdvebGi2EVv8KcqS24tF9Pg7Wh5NLB2NM"
    }
  ],
  "service": [
    {
      "id": "#atproto_pds",
      "type": "AtprotoPersonalDataServer",
      "serviceEndpoint": "https://shimeji.us-east.host.bsky.network"
    }
  ]
}
```

List record collection types for an account:

```bash
$ goat ls -c dril.bsky.social
app.bsky.actor.profile
app.bsky.feed.post
app.bsky.feed.repost
app.bsky.graph.follow
chat.bsky.actor.declaration
```

Fetch a record from the network as JSON:

```bash
$ goat get at://dril.bsky.social/app.bsky.feed.post/3kkreaz3amd27
{
  "$type": "app.bsky.feed.post",
  "createdAt": "2024-02-06T18:15:19.802Z",
  "langs": [
    "en"
  ],
  "text": "I do not Fucking recall them asking the blue sky elders permission to open registration to commoners ."
}
```

Make a public snapshot of your account:

```bash
$ goat repo export jay.bsky.team
downloading from https://morel.us-east.host.bsky.network to: jay.bsky.team.20240811183155.car

$ downloading blobs to: jay.bsky.team_blobs
jay.bsky.team_blobs/bafkreia2x4faux5y7v7v54yl5ebkbaek7z7nhmsd4cooubz3yj4zox34cq	downloaded
jay.bsky.team_blobs/bafkreia3qgbww7odprmysd6jcyxoh5sczkwoxinnmzpsp73gs623fqfm3a	downloaded
jay.bsky.team_blobs/bafkreia3rgnywdrysy65vid42ulyno2cybxhxrn3ragm7cw3smmsxzvbs4	downloaded
[...]
```

Show PLC history for a single account, or make a snapshot of all PLC records (this takes a while), or monitor new ops:

```bash
$ goat plc history atproto.com
[...]

$ goat plc dump | pv -l | gzip > plc_snapshot.json.gz
[...]

$ goat plc dump --cursor now --tail
[...]
```

Verify syntax and generate TIDs:

```bash
$ goat syntax handle check xn--fiqa61au8b7zsevnm8ak20mc4a87e.xn--fiqs8s
valid

$ goat syntax rkey check dHJ1ZQ==
error: recordkey syntax didn't validate via regex

$ goat syntax tid inspect 3kzifvcppte22
Timestamp (UTC): 2024-08-12T02:08:03.29Z
Timestamp (Local): 2024-08-11T19:08:03-07:00
ClockID: 0
uint64: 0x187dcbda2b5ca800
```

The `firehose` commands subscribes to the repo commit stream from a Relay. The default stream outputs event metadata, but doesn't include record blocks (bytes). The `--ops` variant will unpack records and output one line per record operation (instead of one line per commit event), and includes the record values themselves. Some example invocations:

```bash
# possible handle updates
$ goat firehose --account-events | jq .payload.handle
[...]

# text of posts (empty lines for post-deletions)
$ goat firehose - app.bsky.feed.post --ops | jq .record.text
[...]

# sample ratio of languages in current posts
$ goat firehose --ops -c app.bsky.feed.post | head -n100 | jq .record.langs[0] -c | sort | uniq -c | sort -nr
     51 "en"
     33 "ja"
      7 null
      3 "pt"
      2 "ko"
      1 "th"
      1 "id"
      1 "es"
      1 "am"
```

A minimal bsky posting interface, requires account login:

```bash
$ goat bsky post "hello from goat"
```
