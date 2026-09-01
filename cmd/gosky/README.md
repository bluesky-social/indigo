# gosky

A command-line interface for interacting with AT Protocol and Bluesky services.

## Overview

`gosky` is a CLI tool for developers working with the AT Protocol and Bluesky. It provides commands for account management, posting, feed operations, repository synchronization, DID resolution, handle management, and administrative tasks.

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/bluesky-social/indigo.git
cd indigo

# Build and install
go install ./cmd/gosky

# Or run directly
go run ./cmd/gosky [command]
```

## Configuration

### Environment Variables

- `ATP_PDS_HOST` - PDS instance URL (default: `https://bsky.social`)
- `ATP_AUTH_FILE` - Path to authentication file (default: `bsky.auth`)
- `ATP_PLC_HOST` - PLC registry URL (default: `https://plc.directory`)

### Global Flags

- `--pds-host` - Override the PDS host
- `--auth` - Path to authentication JSON file
- `--plc` - PLC registry URL

## Authentication

Before using authenticated commands, you need to create a session:

```bash
# Create a session (login)
gosky account create-session <handle> <password>
```

This will output session information. Save the access token to your auth file (default: `bsky.auth`):

```json
{
  "accessJwt": "your-access-token",
  "refreshJwt": "your-refresh-token",
  "handle": "your.handle",
  "did": "did:plc:..."
}
```

### Refresh Your Session

```bash
gosky account refresh-session
```

## Commands

### Account Management

#### Create a Session (Login)
```bash
gosky account create-session <handle> <password>
```

#### Create a New Account
```bash
gosky account new <email> <handle> <password> [inviteCode]
```

#### Reset Password
```bash
gosky account reset-password <email>
```

#### Request Account Deletion
```bash
gosky account request-deletion
```

#### Delete Account
```bash
gosky account delete <token> <password>
```

### Bluesky Operations

#### Create a Post
```bash
gosky bsky post "Hello from the command line!"
```

#### Follow a User
```bash
gosky bsky follow <did-or-handle>
```

#### List Follows
```bash
# List your follows
gosky bsky list-follows

# List another user's follows
gosky bsky list-follows <actor>
```

#### Get Feed
```bash
# Get your timeline
gosky bsky get-feed

# Get a specific user's feed
gosky bsky get-feed --author <handle>

# Get feed with raw JSON output
gosky bsky get-feed --raw

# Limit number of posts
gosky bsky get-feed --count 50
```

#### Like a Post
```bash
gosky bsky like <post-uri>
```

#### Delete a Post
```bash
gosky bsky delete-post <rkey>
```

#### Get Notifications
```bash
gosky bsky notifs
```

#### Get Suggested Accounts
```bash
gosky bsky actor-get-suggestions
```

### DID Operations

#### Resolve a DID
```bash
# Get DID document
gosky did get <did>

# Resolve DID to handle
gosky did get --handle <did>
```

#### Create a DID
```bash
gosky did create <handle> <service> --signingkey <path> [--recoverydid <did>]
```

#### Get DID from Key
```bash
gosky did did-key --keypath <path>
```

### Handle Operations

#### Resolve a Handle
```bash
gosky handle resolve <handle>
```

#### Update Your Handle
```bash
gosky handle update <new-handle>
```

### Repository Sync

#### Download a Repository
```bash
# Download to default filename (did.car)
gosky sync get-repo <did-or-handle>

# Download to specific file
gosky sync get-repo <did-or-handle> myrepo.car

# Download to stdout
gosky sync get-repo <did-or-handle> -
```

#### Get Repository Root
```bash
gosky sync get-root <did>
```

#### List Repositories
```bash
gosky sync list-repos
```

### Record Operations

#### Get a Record
```bash
# From a DID
gosky get-record --repo <did> <rpath>

# From an AT URI
gosky get-record at://did:plc:.../app.bsky.feed.post/...

# From a Bluesky URL
gosky get-record https://bsky.app/profile/alice.bsky.social/post/...

# Get raw CBOR
gosky get-record --raw --repo <did> <rpath>
```

#### List All Records
```bash
# List all posts (default)
gosky list <did>

# List all record types
gosky list --all <did>

# Show record values
gosky list --values <did>

# Show CIDs
gosky list --cids <did>
```

#### Create a Feed Generator
```bash
gosky create-feed-gen --name <name> --did <did> [--description <desc>] [--display-name <name>]
```

### Stream Operations

#### Subscribe to Repository Stream
```bash
# Basic stream
gosky read-stream <relay-url>

# With JSON output
gosky read-stream --json <relay-url>

# Unpack records
gosky read-stream --unpack <relay-url>

# Resolve handles
gosky read-stream --resolve-handles <relay-url>

# Start from cursor
gosky read-stream <relay-url> <cursor>

# Rate limit consumption
gosky read-stream --max-throughput 100 <relay-url>
```

Example:
```bash
gosky read-stream wss://bsky.network
```

### CAR File Operations

#### Unpack a CAR File
```bash
# Unpack to JSON files
gosky car unpack <car-file>

# Unpack to CBOR files
gosky car unpack --cbor <car-file>

# Specify output directory
gosky car unpack --out-dir <dir> <car-file>
```

### Utilities

#### Parse Record Key Timestamp
```bash
# Get RFC3339 timestamp
gosky parse-rkey <rkey>

# Get Unix timestamp
gosky parse-rkey --format unix <rkey>
```

#### List Labels
```bash
# List labels from the last hour (default)
gosky list-labels

# List labels from a specific time period
gosky list-labels --since 24h
```

### Admin Commands

Admin commands require authentication with admin credentials.

#### Check User
```bash
gosky admin check-user --admin-password <password> <did-or-handle>

# List invited DIDs
gosky admin check-user --list-invited-dids --admin-password <password> <did-or-handle>
```

#### Create Invite Codes
```bash
# Create invites for a user
gosky admin create-invites --admin-password <password> --num 5 <handle>

# Bulk create invites
gosky admin create-invites --admin-password <password> --bulk <file>
```

#### Disable/Enable Invites
```bash
gosky admin disable-invites --admin-password <password> <did-or-handle>
gosky admin enable-invites --admin-password <password> <did-or-handle>
```

#### List Reports
```bash
gosky admin reports list --admin-password <password>
```

#### Account Takedown
```bash
gosky admin account-takedown --admin-password <password> --reason <reason> --admin-user <did> <did>
```

### BGS Admin Commands

Commands for administering a BigGraphStore (BGS/Relay).

```bash
# List upstream connections
gosky bgs list --key <admin-key>

# Kick a connection
gosky bgs kick --key <admin-key> <host>

# Ban a domain
gosky bgs ban-domain --key <admin-key> <domain>

# List domain bans
gosky bgs list-domain-bans --key <admin-key>

# Take down a repository
gosky bgs take-down-repo --key <admin-key> <did>

# Compact a repository
gosky bgs compact-repo --key <admin-key> <did>

# Reset a repository
gosky bgs reset-repo --key <admin-key> <did>
```

### Debug Commands

Advanced debugging utilities for AT Protocol developers.

```bash
# Inspect a specific event
gosky debug inspect-event --host <relay-url> <cursor>

# Debug stream
gosky debug debug-stream --host <relay-url> <cursor>

# Compare two streams
gosky debug compare-streams --host1 <url1> --host2 <url2>

# Debug feed generator
gosky debug debug-feed <at-uri>

# Get and verify a repository
gosky debug get-repo <did>

# Compare repositories from two hosts
gosky debug compare-repos --host-1 <url1> --host-2 <url2> <did>
```

## Examples

### Basic Workflow

```bash
# 1. Create a session
gosky account create-session alice.bsky.social mypassword > bsky.auth

# 2. Create a post
gosky bsky post "Hello from gosky!"

# 3. Get your timeline
gosky bsky get-feed --count 10

# 4. Follow someone
gosky bsky follow bob.bsky.social

# 5. Like a post
gosky bsky like at://did:plc:.../app.bsky.feed.post/...
```

### Working with Repositories

```bash
# Download a repository
gosky sync get-repo alice.bsky.social alice.car

# Unpack it to examine records
gosky car unpack alice.car

# List all records in a repo
gosky list --all --values did:plc:...
```

### Monitoring the Firehose

```bash
# Watch the firehose with unpacked records
gosky read-stream --unpack --resolve-handles wss://bsky.network
```

### Administrative Tasks

```bash
# Check user details
gosky admin check-user --admin-password <pwd> alice.bsky.social

# Create invite codes
gosky admin create-invites --admin-password <pwd> --num 10 alice.bsky.social

# Review moderation reports
gosky admin reports list --admin-password <pwd>
```

## Troubleshooting

### Authentication Errors

If you get authentication errors, try refreshing your session:
```bash
gosky account refresh-session
```

### Connection Issues

Specify a different PDS host:
```bash
gosky --pds-host https://pds.example.com bsky get-feed
```

### Debugging

Use the `--help` flag on any command for detailed usage:
```bash
gosky bsky --help
gosky bsky post --help
```

## Contributing

This tool is part of the [indigo](https://github.com/bluesky-social/indigo) repository. Contributions are welcome!

## License

See the main indigo repository for license information.
