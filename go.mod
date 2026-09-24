module github.com/truvity/policy/examples/url-shortener

go 1.27.0

// The example always builds against the contracts in this checkout, not
// against a published version of them. A change to a schema or a loader that
// would break a service breaks this one in the pull request that made it,
// which is the whole reason the example lives here.
replace github.com/truvity/policy => ../..

require (
	connectrpc.com/connect v1.21.0
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/gofiber/fiber/v3 v3.5.0
	github.com/nats-io/nats.go v1.54.0
	github.com/neilotoole/slogt/v2 v2.0.0
	github.com/stretchr/testify v1.12.1
	github.com/truvity/policy v0.0.0-20260923133553-7af306cdbbcb
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/sync v0.23.0
	google.golang.org/protobuf v1.36.11
	gorm.io/driver/postgres v1.6.3
	gorm.io/gorm v1.31.2
)

require (
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/gofiber/fiber/v2 v2.52.14 // indirect
	github.com/gofiber/schema v1.8.3 // indirect
	github.com/gofiber/utils/v2 v2.4.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/compress v1.20.0 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.73.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)
