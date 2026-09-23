package api

// The paths and identifiers the HTTP surface is built from. They are
// constants rather than literals at the call site because two of them appear
// in more than one place — the route and the chart's probe or route rule —
// and a path that drifts between those is a 404 nobody can explain.
const (
	// PathRedirect prefixes the redirect routes. Short, and distinct from the
	// resource paths, so that a key can never be mistaken for a collection.
	PathRedirect = "/r"

	// PathVersion serves what this binary was built from.
	PathVersion = "/version"

	// PathSuffixRedirect is the key in the redirect route.
	PathSuffixRedirect = "/{url_key}"

	// OpRedirectURL is the operation id of the redirect route, which is what
	// a generated client names the method.
	OpRedirectURL = "RedirectURL"

	// SummaryRedirect is the redirect route's one-line description.
	SummaryRedirect = "Redirect short URL"
)

// SecurityPublic marks a route as needing no credentials. An empty slice
// rather than a nil one: the difference is visible in the generated OpenAPI
// document, where nil means "inherit" and empty means "none, deliberately".
var SecurityPublic = []map[string][]string{}
