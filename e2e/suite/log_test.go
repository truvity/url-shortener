package suite

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	v1 "github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1"
)

// objectStoreService and objectStoreNamespace name the box's S3 stand-in —
// hack/kind's own LocalStack, addressed the same way
// examples/url-shortener/hack/install.sh's LOCAL_STORE_ARGS already does.
// It is not one of this chart's own components either, for the same reason
// Postgres is not: it is the box's server, not something the chart renders.
const (
	objectStoreService   = "s3"
	objectStoreNamespace = "object-store"
	objectStorePort      = 4566

	// requestsPrefix is the archiver's own key prefix — see
	// hack/smoke.sh's identical literal and log/src/url_shortener_log's
	// config for where it comes from.
	requestsPrefix = "url-shortener/requests/"
)

// TestLogArchivesTheRecord proves the fourth language in this example does
// its part: the archiver read the SAME chart-rendered configuration as
// everything else, validated it against a schema carried inside its own
// wheel, bound a durable consumer on the request subject, and wrote a
// record of a redirect that just happened to a store reached by endpoint —
// within its batch window (install.sh sets it to seconds; a real deployment
// may set minutes, hence the generous bound here).
func TestLogArchivesTheRecord(t *testing.T) {
	// Long enough to outlast the 90s polling bound below with margin — a
	// context that expires mid-poll fails every remaining attempt with its
	// OWN error and hides whatever findArchivedRecord was actually seeing.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s3Client := s3ClientOrSkip(ctx, t)

	client := urlsClient(ctx, t)
	longURL := testLongURL(t)
	created, err := client.Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create the URL under test", err))
	}
	key := created.Msg.GetUrl().GetKey()
	_ = followRedirect(ctx, t, key)

	// The redirect record carries the short key (url_key, and the /r/<key>
	// path) rather than the long URL it resolved to — see
	// log/src/url_shortener_log's request schema — so that is what
	// identifies THIS test's record among every other one archived on a
	// standing tenant.
	eventually(t, 90*time.Second, func() error {
		return findArchivedRecord(ctx, s3Client, key)
	})
}

// s3ClientOrSkip builds an S3 client against wherever this environment's
// archive bucket lives.
//
// envS3Endpoint set (a real S3, or S3-compatible, endpoint outside kind)
// takes it. envS3AccessKeyID and envS3SecretAccessKey, set BESIDE it,
// carry STATIC credentials for that endpoint — the url-shortener-e2e
// chart's own way of handing this client the same bucket credentials the
// application itself would read from a Secret (charts/url-shortener's
// archive.bucket.credentialsSecret), for an environment with no ambient
// identity to fall back to: the kind box's own S3 stand-in, reached at a
// real endpoint (http://s3.object-store.svc:4566) but authenticated with
// nothing an IMDS-shaped credential chain can discover. Unset, credentials
// come from this process's own default AWS credential chain instead — an
// in-cluster Job's Pod identity, in production — never the kind box's
// static test credentials, which are meaningless anywhere else.
//
// envS3Endpoint unset falls back to the kind box's own S3 stand-in
// (object-store/s3, reached through the harness the same way every other
// Service in this suite is), with the same static test credentials
// hack/install.sh's BUCKET_SECRET carries — the local box has no identity
// plane to hand the client instead (docs/decisions/0005-kind-is-the-gate.md's
// amendment). Outside kind, that Service does not exist, so this SKIPS
// rather than fails: reading a bucket from outside the application's own
// IAM is a permission a caller should not be handed just to run this one
// check, and the operator can opt in by setting envS3Endpoint (see
// env_test.go).
//
// namesFromEnvMode's own Job never even ATTEMPTS the lookup: the same
// reasoning as postgresHostPort's (db_test.go) — there is no reason to
// believe a Service named objectStoreService in objectStoreNamespace exists
// off the kind box, and this Job's Role grants it no permission to look one
// up in a namespace that is not its own release's in any case. Skipping
// here, before the harness ever shells out, keeps a Forbidden
// `kubectl get services` off this Job's log entirely — the harness's own
// runner streams a subprocess's stderr straight through (so the operator
// sees interactive credential prompts and `kubectl`/`helm` progress), which
// otherwise means the raw "Error from server (Forbidden)" line lands right
// before this very skip and reads like a failure to anyone scanning a green
// run.
func s3ClientOrSkip(ctx context.Context, t *testing.T) *s3.Client {
	t.Helper()

	if endpoint := strings.TrimSpace(os.Getenv(envS3Endpoint)); endpoint != "" {
		region := getenv(envS3Region, defaultS3Region)

		accessKeyID := strings.TrimSpace(os.Getenv(envS3AccessKeyID))
		secretAccessKey := strings.TrimSpace(os.Getenv(envS3SecretAccessKey))
		if accessKeyID != "" && secretAccessKey != "" {
			return s3.New(s3.Options{
				Region:       region,
				BaseEndpoint: aws.String(endpoint),
				UsePathStyle: true,
				Credentials:  credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
			})
		}

		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			t.Fatalf("load the default AWS config for %s (%s=%s): %v", region, envS3Endpoint, endpoint, err)
		}
		return s3.New(s3.Options{
			Region:       region,
			BaseEndpoint: aws.String(endpoint),
			Credentials:  cfg.Credentials,
		})
	}

	if namesFromEnvMode() {
		t.Skipf("%s is set and %s is not — this Job's Role grants no permission to look up "+
			"%s/%s, and there is no reason to believe that Service exists off the kind box: "+
			"skipping the archive check", envNamesFromEnv, envS3Endpoint, objectStoreNamespace, objectStoreService)
	}

	endpoint, err := shared.cluster.ServiceURL(ctx, objectStoreNamespace, objectStoreService, objectStorePort)
	if err != nil {
		t.Skipf("no %s/%s Service and %s is not set — this environment exposes no object store "+
			"for the archive check: %v", objectStoreNamespace, objectStoreService, envS3Endpoint, err)
	}

	return s3.New(s3.Options{
		Region:       defaultS3Region,
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
	})
}

// findArchivedRecord lists every object the archiver has written and reads
// the ones under requestsPrefix looking for one that carries key — an
// OBJECT existing is not the same as a RECORD of the right redirect: an
// empty object, or one holding somebody else's events, would pass a check
// for "a key exists" (a claim the identically-named short "key" makes this
// sentence read strangely — the log record's own field is url_key). It
// returns an error (never fails the test directly) so eventually can poll
// it.
func findArchivedRecord(ctx context.Context, client *s3.Client, key string) error {
	list, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(shared.names.Bucket),
		Prefix: aws.String(requestsPrefix),
	})
	if err != nil {
		return wrapObjectStoreErr(err)
	}
	if len(list.Contents) == 0 {
		return errNoObjects
	}

	for _, obj := range list.Contents {
		body, err := client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(shared.names.Bucket),
			Key:    obj.Key,
		})
		if err != nil {
			return wrapObjectStoreErr(err)
		}
		raw, err := io.ReadAll(body.Body)
		_ = body.Body.Close()
		if err != nil {
			return wrapObjectStoreErr(err)
		}
		if strings.Contains(string(raw), `"url_key":"`+key+`"`) &&
			strings.Contains(string(raw), `"subject":"`+shared.names.RequestSubject+`"`) {
			return nil
		}
	}

	return errNotArchivedYet
}

var (
	errNoObjects      = archiveError("nothing archived yet")
	errNotArchivedYet = archiveError("no archived object carries this redirect's record yet")
)

type archiveError string

func (e archiveError) Error() string { return string(e) }

// wrapObjectStoreErr enriches an S3 call's error with the forward it went
// through, the same way wrapThroughForward does for every HTTP call in this
// suite.
func wrapObjectStoreErr(err error) error {
	if fw, ok := shared.cluster.ForwardFor(objectStoreNamespace, objectStoreService); ok {
		return fw.Err(err)
	}
	return err
}
