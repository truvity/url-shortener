// Command resolve prints the names fixture.Resolve computes, as shell
// variable assignments — `NAME=value`, one per line, values shell-quoted —
// so that apply.sh and hack/install.sh can `eval "$(go run ./e2e/fixture/cmd/resolve ...)"`
// and use the SAME names rather than each maintaining its own copy of the
// convention.
//
// Flags mirror fixture.Options; every one has the default apply.sh and
// hack/install.sh both rely on when it is left unset.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/truvity/policy/examples/url-shortener/e2e/fixture"
)

func main() {
	d := fixture.DefaultOptions()
	namespace := flag.String("namespace", d.Namespace, "the namespace the application chart installs into")
	appRelease := flag.String("app-release", d.AppRelease, "the application chart's release name")
	bucket := flag.String("bucket", d.Bucket, "the archive bucket's name")
	flag.Parse()

	names, err := fixture.Resolve(fixture.Options{
		Namespace:  *namespace,
		AppRelease: *appRelease,
		Bucket:     *bucket,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve:", err)
		os.Exit(1)
	}

	print("NAMESPACE", names.Namespace)
	print("APP_RELEASE", names.AppRelease)
	print("DATABASE", names.Database)
	print("OWNER_ROLE", names.OwnerRole)
	print("APP_ROLE", names.AppRole)
	print("DATABASE_HOST", names.DatabaseHost)
	print("OWNER_SECRET", names.OwnerSecret)
	print("APP_SECRET", names.AppSecret)
	print("STREAM", names.Stream)
	print("SUBJECTS", strings.Join(names.Subjects, ","))
	print("REDIRECT_SUBJECT", names.RedirectSubject)
	print("REQUEST_SUBJECT", names.RequestSubject)
	print("STAT_CONSUMER", names.StatConsumer)
	print("LOG_CONSUMER", names.LogConsumer)
	print("BUCKET", names.Bucket)
}

// print writes one `NAME='value'` line. Single-quoted: every value here is
// a Kubernetes or NATS name, which cannot itself contain a single quote, so
// this does not need to handle escaping one.
func print(name, value string) {
	fmt.Printf("%s=%s\n", name, shellQuote(value))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
