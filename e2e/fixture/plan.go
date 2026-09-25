package fixture

// Provides is the set of names this fixture creates, keyed by what each one
// is — a role, a Secret, a stream, a subject, a consumer or a bucket — so a
// caller comparing "what the chart would create" against "what this fixture
// provides" is comparing two sets of the same shape rather than guessing
// which field means what.
//
// apply.sh reads this through cmd/resolve rather than a copy of it, and
// names_test.go asserts it against an INDEPENDENT render of the chart —
// see that test for why a name here is not simply read back from itself.
func (n Names) Provides() map[string]string {
	return map[string]string{
		"role:owner":       n.OwnerRole,
		"role:app":         n.AppRole,
		"secret:owner":     n.OwnerSecret,
		"secret:app":       n.AppSecret,
		"stream":           n.Stream,
		"subject:redirect": n.RedirectSubject,
		"subject:request":  n.RequestSubject,
		"consumer:stat":    n.StatConsumer,
		"consumer:log":     n.LogConsumer,
		"bucket":           n.Bucket,
	}
}
