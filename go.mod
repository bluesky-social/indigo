module github.com/bluesky-social/indigo

go 1.21

require (
	contrib.go.opencensus.io/exporter/prometheus v0.4.2
	filippo.io/age v1.1.1
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de
	github.com/benbjohnson/litestream v0.3.13
	github.com/brianvoe/gofakeit/v6 v6.20.2
	github.com/carlmjohnson/versioninfo v0.22.5
	github.com/deckarep/golang-set/v2 v2.3.1
	github.com/dustinkirkland/golang-petname v0.0.0-20230626224747-e794b9370d49
	github.com/flosch/pongo2/v6 v6.0.0
	github.com/fsnotify/fsnotify v1.7.0
	github.com/goccy/go-json v0.10.2
	github.com/gocql/gocql v1.6.0
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/gorilla/websocket v1.5.0
	github.com/hashicorp/go-retryablehttp v0.7.2
	github.com/hashicorp/golang-lru v0.5.4
	github.com/hashicorp/golang-lru/arc/v2 v2.0.6
	github.com/hashicorp/golang-lru/v2 v2.0.6
	github.com/icrowley/fake v0.0.0-20221112152111-d7b7e2276db2
	github.com/ipfs/go-block-format v0.1.2
	github.com/ipfs/go-bs-sqlite3 v0.0.0-20221122195556-bfcee1be620d
	github.com/ipfs/go-cid v0.4.1
	github.com/ipfs/go-datastore v0.6.0
	github.com/ipfs/go-ds-flatfs v0.5.1
	github.com/ipfs/go-ipfs-blockstore v1.3.0
	github.com/ipfs/go-ipld-cbor v0.0.7-0.20230126201833-a73d038d90bc
	github.com/ipfs/go-ipld-format v0.4.0
	github.com/ipfs/go-libipfs v0.7.0
	github.com/ipfs/go-log v1.0.5
	github.com/ipfs/go-log/v2 v2.5.1
	github.com/ipld/go-car v0.6.1-0.20230509095817-92d28eb23ba4
	github.com/ipld/go-car/v2 v2.9.0
	github.com/jackc/pgx/v5 v5.3.0
	github.com/joho/godotenv v1.5.1
	github.com/labstack/echo-contrib v0.15.0
	github.com/labstack/echo/v4 v4.11.1
	github.com/labstack/gommon v0.4.0
	github.com/lestrrat-go/jwx/v2 v2.0.12
	github.com/minio/sha256-simd v1.0.0
	github.com/mitchellh/go-homedir v1.1.0
	github.com/mr-tron/base58 v1.2.0
	github.com/multiformats/go-multihash v0.2.1
	github.com/opensearch-project/opensearch-go/v2 v2.2.0
	github.com/polydawn/refmt v0.89.1-0.20221221234430-40501e09de1f
	github.com/prometheus/client_golang v1.16.0
	github.com/prometheus/client_model v0.4.0
	github.com/rivo/uniseg v0.1.0
	github.com/samber/slog-echo v1.2.1
	github.com/scylladb/gocqlx/v2 v2.8.1-0.20230309105046-dec046bd85e6
	github.com/stretchr/testify v1.8.4
	github.com/urfave/cli/v2 v2.25.1
	github.com/whyrusleeping/cbor-gen v0.0.0-20230818171029-f91ae536ca25
	github.com/whyrusleeping/go-did v0.0.0-20230824162731-404d1707d5d6
	gitlab.com/yawning/secp256k1-voi v0.0.0-20230815035612-a7264edccf80
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.42.0
	go.opentelemetry.io/otel v1.19.0
	go.opentelemetry.io/otel/exporters/jaeger v1.14.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.19.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.19.0
	go.opentelemetry.io/otel/sdk v1.19.0
	go.opentelemetry.io/otel/trace v1.19.0
	go.uber.org/automaxprocs v1.5.3
	go.uber.org/zap v1.24.0
	golang.org/x/crypto v0.13.0
	golang.org/x/exp v0.0.0-20230321023759-10a507213a29
	golang.org/x/sync v0.3.0
	golang.org/x/text v0.13.0
	golang.org/x/time v0.3.0
	golang.org/x/tools v0.8.0
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2
	gorm.io/driver/postgres v1.5.0
	gorm.io/driver/sqlite v1.5.0
	gorm.io/gorm v1.25.1
	gorm.io/plugin/opentelemetry v0.1.3
)

require (
	cloud.google.com/go v0.110.7 // indirect
	cloud.google.com/go/compute v1.23.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	cloud.google.com/go/iam v1.1.1 // indirect
	cloud.google.com/go/storage v1.31.0 // indirect
	github.com/Azure/azure-pipeline-go v0.2.3 // indirect
	github.com/Azure/azure-storage-blob-go v0.15.0 // indirect
	github.com/aws/aws-sdk-go v1.44.318 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/s2a-go v0.1.4 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.5 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mattn/go-ieproxy v0.0.11 // indirect
	github.com/pierrec/lz4/v4 v4.1.18 // indirect
	github.com/pkg/sftp v1.13.5 // indirect
	golang.org/x/oauth2 v0.11.0 // indirect
	google.golang.org/api v0.135.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230807174057-1744710a1577 // indirect
)

require (
	github.com/alexbrainman/goissue34681 v0.0.0-20191006012335-3fc7a47baff5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.2.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/corpix/uarand v0.0.0-20170723150923-031be390f409 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/felixge/httpsnoop v1.0.3 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.16.0 // indirect
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/ipfs/bbloom v0.0.4 // indirect
	github.com/ipfs/go-blockservice v0.5.0 // indirect
	github.com/ipfs/go-ipfs-ds-help v1.1.0 // indirect
	github.com/ipfs/go-ipfs-exchange-interface v0.2.0 // indirect
	github.com/ipfs/go-ipfs-util v0.0.2 // indirect
	github.com/ipfs/go-ipld-legacy v0.1.1 // indirect
	github.com/ipfs/go-merkledag v0.10.0 // indirect
	github.com/ipfs/go-metrics-interface v0.0.1 // indirect
	github.com/ipfs/go-verifcid v0.0.2 // indirect
	github.com/ipld/go-codec-dagpb v1.6.0 // indirect
	github.com/ipld/go-ipld-prime v0.20.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jbenet/goprocess v0.1.4 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/cpuid/v2 v2.2.3 // indirect
	github.com/lestrrat-go/blackmagic v1.0.1 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.4 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/mattn/go-sqlite3 v1.14.17 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.8.1 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/common v0.44.0 // indirect
	github.com/prometheus/procfs v0.11.1 // indirect
	github.com/prometheus/statsd_exporter v0.22.7 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/samber/lo v1.38.1 // indirect
	github.com/scylladb/go-reflectx v1.0.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	gitlab.com/yawning/tuplehash v0.0.0-20230713102510-df83abbf9a02 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho v0.45.0
	go.opentelemetry.io/otel/metric v1.19.0 // indirect
	go.opentelemetry.io/proto/otlp v1.0.0 // indirect
	go.uber.org/atomic v1.10.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/mod v0.10.0 // indirect
	golang.org/x/net v0.15.0 // indirect
	golang.org/x/sys v0.12.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20230807174057-1744710a1577 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230807174057-1744710a1577 // indirect
	google.golang.org/grpc v1.58.2 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	lukechampine.com/blake3 v1.1.7 // indirect
)
