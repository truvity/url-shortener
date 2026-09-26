package suite

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/aws/aws-sdk-go-v2/aws"
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

	client := urlsClient(ctx, t)
	longURL := testLongURL(t)
	created, err := client.Create(ctx, connect.NewRequest(&v1.CreateRequest{LongUrl: longURL}))
	if err != nil {
		t.Fatalf("%s", errString(componentURLs, "create the URL under test", err))
	}
	key := created.Msg.GetUrl().GetKey()
	_ = followRedirect(ctx, t, key)

	s3Client := newS3Client(ctx, t)

	// The redirect record carries the short key (url_key, and the /r/<key>
	// path) rather than the long URL it resolved to — see
	// log/src/url_shortener_log's request schema — so that is what
	// identifies THIS test's record among every other one archived on a
	// standing tenant.
	eventually(t, 90*time.Second, func() error {
		return findArchivedRecord(ctx, s3Client, key)
	})
}

// newS3Client builds an S3 client against the box's LocalStack, through the
// harness — the same ServiceURL every other Service in this suite goes
// through. Static test credentials, matching hack/install.sh's own
// BUCKET_SECRET: the local box has no identity plane to hand the client
// instead (docs/decisions/0005-kind-is-the-gate.md's amendment).
func newS3Client(ctx context.Context, t *testing.T) *s3.Client {
	t.Helper()

	endpoint, err := shared.cluster.ServiceURL(ctx, objectStoreNamespace, objectStoreService, objectStorePort)
	if err != nil {
		t.Fatalf("resolve the object-store Service: %v", err)
	}

	return s3.New(s3.Options{
		Region:       "us-east-1",
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
