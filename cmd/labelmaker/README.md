
labelmaker
===========

## Database Setup

PostgreSQL and Sqlite are both supported. When using Sqlite, separate database
for the BGS database itself and the CarStore are used. With PostgreSQL a single
database server, user, and database, can all be reused.

Database configuration is passed via the `DATABASE_URL` and
`CARSTORE_DATABASE_URL` environment variables, or the corresponding CLI args.

For PostgreSQL, the user and database must already be configured. Some example
SQL commands are:

    CREATE DATABASE bgs;
    CREATE DATABASE carstore;

    CREATE USER ${username} WITH PASSWORD '${password}';
    GRANT ALL PRIVILEGES ON DATABASE bgs TO ${username};
    GRANT ALL PRIVILEGES ON DATABASE carstore TO ${username};

This service currently uses `gorm` to automatically run database migrations as
the regular user. There is no concept of running a separate set of migrations
under more privileged database user.

For database performance with many labels, it is important that `LC_COLLATE=C`.
That is, the string sort behavior must be by byte order.

## Keyword Labeler

A trivial keyword filter labeler is included. To configure it, create a JSON
with the same structure as the `example_keywords.json` file in this directory,
and provide the path to the `--keyword-file` CLI arg (or the corresponding env
var).

The structure is a list of label values ("value"), each with a list of
lower-case keyword tokens. If a token is found in post or profile text, the
corresponding label is generated.


## micro-NSFW-img Integration

`micro_nsfw_img` is a simple image classification tool, useful for integration
testing and local development. You can HTTP POST and image to it and get a set
of floating point scores back about whether it is hentai, porn, etc. See more
at <https://gitlab.com/bnewbold/micro-nsfw-img>.

To get it working with labelmaker, download the huge (3+ GByte) dockerfile and
run it locally:

    docker pull bnewbold/micro-nsfw-img:latest
    docker run --network host bnewbold/micro-nsfw-img

Then configure labelmaker with:

    # or the '--micro-nsfw-img-url' CLI flag
    LABELMAKER_MICRO_NSFW_IMG_URL="http://localhost:5000/classify-image"


## SQRL Integration

SQRL is a moderation system built around a declarative rule language,
application events, and cached counter values. It is the open source release of
Smyt, a moderation system aquired and used by Twitter many years ago. See the
SQRL docs for more: <https://sqrl-lang.github.io/sqrl/index.html>

A local SQRL moderation server can be queried by providing `--sqrl-url` (or the
corresponding env var). Post and Profile records will be passed, wrapped in a
top-level JSON field `EventData`.

An example SQRL ruleset for posts and profiles is provided in `sqrl_example`.
To use this, checkout the SQRL codebase and get it running, then copy the
`bsky` folder to the top directory and run:

    ./sqrl serve bsky/main.sqrl

Counter state will not persist across restarts unless Redis is configured as
well.


## Repo Account Setup

You'll need a DID and handle for the labelmaker service itself.

Generate the secret keys (as JSON files), along with did:key representations,
and store these in a password manager:

    go run ./cmd/laputa/ gen-key -o labelmaker_signing.key
    go run ./cmd/gosky/ did didKey --keypath labeler_signing.key

    go run ./cmd/laputa/ gen-key -o labelmaker_recovery.key
    go run ./cmd/gosky/ did didKey --keypath labeler_recovery.key

Use the result to generate a new DID:

    go run ./cmd/gosky/ did create --recoverydid did:key:FROMABOVE --signingkey labeler_signing.key your.handle.tld https://your.pds.host

The signing key JSON, along with repo handle and DID, can be passed to
labelmaker via an environment variables.
