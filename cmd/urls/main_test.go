package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"
)

// A panic reaches the caller as an ordinary error with a code, not as a
// closed stream, and the panic itself stays out of the answer.
//
// What arrived before this existed was `Stream closed with error code
// NGHTTP2_INTERNAL_ERROR`: a transport failure naming nothing, for a request
// that had already written its row.
func TestAPanicIsAnOrdinaryErrorAndNotAnAnswer(t *testing.T) {
	var logged strings.Builder
	log := slog.New(slog.NewTextHandler(&logged, nil))

	err := recoverToError(log)(context.Background(), connect.Spec{Procedure: "/x.Y/Z"}, http.Header{}, "secret detail: db password is hunter2")

	if got := connect.CodeOf(err); got != connect.CodeInternal {
		t.Errorf("code = %v, want internal", got)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the panic value reached the CALLER: %v", err)
	}
	// Loud where it is useful: the operator has the procedure and the
	// value, or a bug fixed by the net's existence would go unseen.
	for _, want := range []string{"handler panicked", "/x.Y/Z", "hunter2"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("the log does not carry %q:\n%s", want, logged.String())
		}
	}
}
