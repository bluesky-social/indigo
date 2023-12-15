
## Running `fakermaker`

Configure a `.env` file for use against `atproto` Typescript PDS implementation
(in development mode, already running locally):

	ATP_PDS_HOST=http://localhost:2583
	ATP_AUTH_HANDLE="admin.test"
	ATP_AUTH_PASSWORD="admin"
	ATP_AUTH_ADMIN_PASSWORD="admin"

or, against `laputa` golang PDS implementation (in this repo; already running
locally):

	ATP_PDS_HOST=http://localhost:4989
	ATP_AUTH_HANDLE="admin.test"
	ATP_AUTH_PASSWORD="admin"
	ATP_AUTH_ADMIN_PASSWORD="admin"

Then, from the top-level directory, run test commands:

	mkdir -p data/fakermaker
    export GOLOG_LOG_LEVEL=info

    # setup and create initial accounts; 100 by default
    # supply --use-invite-code and/or --domain-suffix SUFFIX as needed
	go run ./cmd/fakermaker/ gen-accounts > data/fakermaker/accounts.json

    # create or update profiles for all the accounts
    go run ./cmd/fakermaker/ gen-profiles

    # create follow graph between accounts
    go run ./cmd/fakermaker/ gen-graph

    # create posts, including mentions and image uploads
    go run ./cmd/fakermaker/ gen-posts

    # create more interactions, such as likes, between accounts
    go run ./cmd/fakermaker/ gen-interactions

    # lastly, read-only queries, including timelines, notifications, and post threads
    go run ./cmd/fakermaker/ run-browsing                                                                               


## Docker Compose Integration Tests

To run against Typescript services running in Docker, use the docker compose
file in this directory.

Run all the servics:

    docker-compose up

Then configure and run `fakermaker` using the commands above. To run automated integration tests:

    # from top-level directory of this repo
    make test-interop

If you need to wipe volumes (all databases):

    docker-compose down -v
