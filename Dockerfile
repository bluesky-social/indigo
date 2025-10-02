FROM golang:1.24-bookworm

RUN apt update; apt install -y dumb-init ca-certificates runit    bash-completion git make curl

# add/install websocket client wsdump as alternatives of websocat described in HACKING.md
# RUN apt install -y cargo libssl-dev; cargo install --features=ssl websocat --root /usr/local
RUN go install github.com/nrxr/wsdump@latest

WORKDIR /bluesky-indigo
COPY . .

RUN make build

CMD tail -f /dev/null
ENV PATH .:/bluesky-indigo:${PATH}
