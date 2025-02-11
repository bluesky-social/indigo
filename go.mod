module github.com/bluesky-social/indigo

go 1.23

require (
	contrib.go.opencensus.io/exporter/prometheus v0.4.2
	github.com/PuerkitoBio/purell v1.2.1
	github.com/RussellLuo/slidingwindow v0.0.0-20200528002341-535bb99d338b
	github.com/adrg/xdg v0.5.0
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de
	github.com/bradfitz/gomemcache v0.0.0-20230905024940-24af94b03874
	github.com/brianvoe/gofakeit/v6 v6.25.0
	github.com/carlmjohnson/versioninfo v0.22.5
	github.com/cockroachdb/pebble v1.1.2
	github.com/dustinkirkland/golang-petname v0.0.0-20231002161417-6a283f1aaaf2
	github.com/flosch/pongo2/v6 v6.0.0
	github.com/go-redis/cache/v9 v9.0.0
	github.com/goccy/go-json v0.10.2
	github.com/golang-jwt/jwt v3.2.2+incompatible
	github.com/gorilla/websocket v1.5.1
	github.com/hashicorp/go-retryablehttp v0.7.5
	github.com/hashicorp/golang-lru/arc/v2 v2.0.6
	github.com/hashicorp/golang-lru/v2 v2.0.7
	github.com/icrowley/fake v0.0.0-20221112152111-d7b7e2276db2
	github.com/ipfs/go-block-format v0.2.0
	github.com/ipfs/go-bs-sqlite3 v0.0.0-20221122195556-bfcee1be620d
	github.com/ipfs/go-cid v0.4.1
	github.com/ipfs/go-datastore v0.6.0
	github.com/ipfs/go-ds-flatfs v0.5.1
	github.com/ipfs/go-ipfs-blockstore v1.3.1
	github.com/ipfs/go-ipld-cbor v0.1.0
	github.com/ipfs/go-ipld-format v0.6.0
	github.com/ipfs/go-libipfs v0.7.0
	github.com/ipfs/go-log/v2 v2.5.1
	github.com/ipld/go-car v0.6.1-0.20230509095817-92d28eb23ba4
	github.com/ipld/go-car/v2 v2.13.1
	github.com/jackc/pgx/v5 v5.5.0
	github.com/joho/godotenv v1.5.1
	github.com/labstack/echo-contrib v0.15.0
	github.com/labstack/echo/v4 v4.11.3
	github.com/lestrrat-go/jwx/v2 v2.0.12
	github.com/lib/pq v1.10.9
	github.com/minio/sha256-simd v1.0.1
	github.com/mr-tron/base58 v1.2.0
	github.com/multiformats/go-multihash v0.2.3
	github.com/opensearch-project/opensearch-go/v2 v2.3.0
	github.com/orandin/slog-gorm v1.3.2
	github.com/polydawn/refmt v0.89.1-0.20221221234430-40501e09de1f
	github.com/prometheus/client_golang v1.17.0
	github.com/prometheus/client_model v0.5.0
	github.com/puzpuzpuz/xsync/v3 v3.0.2
	github.com/redis/go-redis/v9 v9.3.0
	github.com/rivo/uniseg v0.1.0
	github.com/samber/slog-echo v1.8.0
	github.com/stretchr/testify v1.9.0
	github.com/urfave/cli/v2 v2.25.7
	github.com/whyrusleeping/cbor-gen v0.2.1-0.20241030202151-b7a6831be65e
	github.com/whyrusleeping/go-did v0.0.0-20230824162731-404d1707d5d6
	github.com/xlab/treeprint v1.2.0
	gitlab.com/yawning/secp256k1-voi v0.0.0-20230925100816-f2616030848b
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.46.1
	go.opentelemetry.io/otel v1.21.0
	go.opentelemetry.io/otel/exporters/jaeger v1.14.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.21.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.21.0
	go.opentelemetry.io/otel/sdk v1.21.0
	go.opentelemetry.io/otel/trace v1.21.0
	go.uber.org/automaxprocs v1.5.3
	go.uber.org/zap v1.26.0
	golang.org/x/crypto v0.21.0
	golang.org/x/sync v0.7.0
	golang.org/x/text v0.14.0
	golang.org/x/time v0.3.0
	golang.org/x/tools v0.15.0
	golang.org/x/xerrors v0.0.0-20231012003039-104605ab7028
	gorm.io/driver/postgres v1.5.7
	gorm.io/driver/sqlite v1.5.5
	gorm.io/gorm v1.25.9
	gorm.io/plugin/opentelemetry v0.1.3
)

require (
	github.com/DataDog/zstd v1.4.5 // indirect
	github.com/cockroachdb/errors v1.11.3 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240606204812-0bbfbd93a7ce // indirect
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b // indirect
	github.com/cockroachdb/redact v1.1.5 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20230807174530-cc333fc44b06 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/getsentry/sentry-go v0.27.0 // indirect
	github.com/go-redis/redis v6.15.9+incompatible // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/hailocab/go-hostpool v0.0.0-20160125115350-e80d13ce29ed // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/ipfs/go-log v1.0.5 // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/klauspost/compress v1.17.3 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/labstack/gommon v0.4.1 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/petar/GoLLRB v0.0.0-20210522233825-ae3b015fd3e9 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rogpeppe/go-internal v1.10.0 // indirect
	github.com/vmihailenco/go-tinylfu v0.2.2 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/whyrusleeping/cbor v0.0.0-20171005072247-63513f603b11 // indirect
	golang.org/x/exp v0.0.0-20231110203233-9a3e6036ecaa // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
)

require (
	github.com/alexbrainman/goissue34681 v0.0.0-20191006012335-3fc7a47baff5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.2.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/corpix/uarand v0.2.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.3 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/uuid v1.4.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.18.1 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2
	github.com/ipfs/bbloom v0.0.4 // indirect
	github.com/ipfs/go-blockservice v0.5.2 // indirect
	github.com/ipfs/go-ipfs-ds-help v1.1.1 // indirect
	github.com/ipfs/go-ipfs-exchange-interface v0.2.1 // indirect
	github.com/ipfs/go-ipfs-util v0.0.3 // indirect
	github.com/ipfs/go-ipld-legacy v0.2.1 // indirect
	github.com/ipfs/go-merkledag v0.11.0 // indirect
	github.com/ipfs/go-metrics-interface v0.0.1 // indirect
	github.com/ipfs/go-verifcid v0.0.3 // indirect
	github.com/ipld/go-codec-dagpb v1.6.0 // indirect
	github.com/ipld/go-ipld-prime v0.21.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jbenet/goprocess v0.1.4 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/lestrrat-go/blackmagic v1.0.1 // indirect
	github.com/lestrrat-go/httpcc v1.0.1 // indirect
	github.com/lestrrat-go/httprc v1.0.4 // indirect
	github.com/lestrrat-go/iter v1.0.2 // indirect
	github.com/lestrrat-go/option v1.0.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-sqlite3 v1.14.22
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.9.0 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/prometheus/statsd_exporter v0.25.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/samber/lo v1.38.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	gitlab.com/yawning/tuplehash v0.0.0-20230713102510-df83abbf9a02 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho v0.45.0
	go.opentelemetry.io/otel/metric v1.21.0 // indirect
	go.opentelemetry.io/proto/otlp v1.0.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/mod v0.14.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20231120223509-83a465c0220f // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231120223509-83a465c0220f // indirect
	google.golang.org/grpc v1.59.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)
