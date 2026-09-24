// Package urls is the RPC boundary over the URL tables.
//
// It is thin on purpose: the store client already holds the queries, and a
// handler that reimplemented them would be a second place for the schema to
// live. What this package adds is the part a boundary owes its callers —
// turning a storage error into a code a client can act on, and refusing a
// request that is malformed before it reaches a query.
package urls

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"

	"github.com/truvity/policy/examples/url-shortener/internal/business/store"
	v1 "github.com/truvity/policy/examples/url-shortener/internal/gen/urlshortener/v1"
)

// KeyLength is how long a short code is. The column holds exactly this many
// characters, so a longer key is a truncation and a shorter one collides
// with nothing — both are refused here rather than at the database, because
// a constraint violation reaches a caller as "internal" and says nothing.
const KeyLength = 8

// Handler serves UrlsService and MetaService over the store.
type Handler struct {
	log     *slog.Logger
	store   *store.Client
	version func() (string, string)
	name    string
}

// New returns a handler over an established store client.
func New(log *slog.Logger, client *store.Client, name string, version func() (string, string)) *Handler {
	return &Handler{log: log, store: client, name: name, version: version}
}

// GetVersion answers what this process is.
func (h *Handler) GetVersion(
	_ context.Context,
	_ *connect.Request[v1.GetVersionRequest],
) (*connect.Response[v1.GetVersionResponse], error) {
	version, commit := h.version()
	return connect.NewResponse(&v1.GetVersionResponse{
		Component: h.name,
		Version:   version,
		Commit:    commit,
	}), nil
}

// Create records a new short URL.
func (h *Handler) Create(
	ctx context.Context,
	req *connect.Request[v1.CreateRequest],
) (*connect.Response[v1.CreateResponse], error) {
	if err := validKey(req.Msg.GetKey()); err != nil {
		return nil, err
	}
	if req.Msg.GetLongUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("long_url is required"))
	}

	expires := optionalTime(req.Msg.GetExpiresAt())
	if err := h.store.PutURL(ctx, req.Msg.GetKey(), req.Msg.GetLongUrl(), expires); err != nil {
		return nil, storeError("create", err)
	}
	return h.getResponse(ctx, req.Msg.GetKey(), func(url *v1.Url) *connect.Response[v1.CreateResponse] {
		return connect.NewResponse(&v1.CreateResponse{Url: url})
	})
}

// Get reports one URL.
func (h *Handler) Get(
	ctx context.Context,
	req *connect.Request[v1.GetRequest],
) (*connect.Response[v1.GetResponse], error) {
	if err := validKey(req.Msg.GetKey()); err != nil {
		return nil, err
	}
	url, err := h.url(ctx, req.Msg.GetKey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.GetResponse{Url: url}), nil
}

// Update changes what a key points at, or when it expires.
func (h *Handler) Update(
	ctx context.Context,
	req *connect.Request[v1.UpdateRequest],
) (*connect.Response[v1.UpdateResponse], error) {
	if err := validKey(req.Msg.GetKey()); err != nil {
		return nil, err
	}
	var longURL *string
	if req.Msg.LongUrl != nil {
		value := req.Msg.GetLongUrl()
		if value == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("long_url was set to empty: omit it to leave it unchanged"))
		}
		longURL = &value
	}
	if _, err := h.store.UpdateURL(ctx, req.Msg.GetKey(), longURL, optionalTime(req.Msg.GetExpiresAt())); err != nil {
		return nil, storeError("update", err)
	}
	url, err := h.url(ctx, req.Msg.GetKey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.UpdateResponse{Url: url}), nil
}

// Delete retires a key. It is never reused, so this is a soft delete and the
// row stays.
func (h *Handler) Delete(
	ctx context.Context,
	req *connect.Request[v1.DeleteRequest],
) (*connect.Response[v1.DeleteResponse], error) {
	if err := validKey(req.Msg.GetKey()); err != nil {
		return nil, err
	}
	if err := h.store.DeleteURL(ctx, req.Msg.GetKey()); err != nil {
		return nil, storeError("delete", err)
	}
	return connect.NewResponse(&v1.DeleteResponse{}), nil
}

// Restore un-retires a key.
func (h *Handler) Restore(
	ctx context.Context,
	req *connect.Request[v1.RestoreRequest],
) (*connect.Response[v1.RestoreResponse], error) {
	if err := validKey(req.Msg.GetKey()); err != nil {
		return nil, err
	}
	if err := h.store.RestoreURL(ctx, req.Msg.GetKey()); err != nil {
		return nil, storeError("restore", err)
	}
	url, err := h.url(ctx, req.Msg.GetKey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.RestoreResponse{Url: url}), nil
}

// Resolve answers where a key points.
func (h *Handler) Resolve(
	ctx context.Context,
	req *connect.Request[v1.ResolveRequest],
) (*connect.Response[v1.ResolveResponse], error) {
	if err := validKey(req.Msg.GetKey()); err != nil {
		return nil, err
	}
	longURL, err := h.store.GetURLString(ctx, req.Msg.GetKey())
	if err != nil {
		return nil, storeError("resolve", err)
	}
	return connect.NewResponse(&v1.ResolveResponse{LongUrl: longURL}), nil
}

// RecordClick counts a redirect that already happened.
func (h *Handler) RecordClick(
	ctx context.Context,
	req *connect.Request[v1.RecordClickRequest],
) (*connect.Response[v1.RecordClickResponse], error) {
	if req.Msg.GetLongUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("long_url is required"))
	}
	if err := h.store.IncrementClickCount(ctx, req.Msg.GetLongUrl()); err != nil {
		return nil, storeError("record click", err)
	}
	info, err := h.store.FindURLByLongURL(ctx, req.Msg.GetLongUrl())
	if err != nil {
		return nil, storeError("read back the count", err)
	}
	count, err := h.store.GetClickCount(ctx, info.URLKey)
	if err != nil {
		return nil, storeError("read back the count", err)
	}
	return connect.NewResponse(&v1.RecordClickResponse{ClickCount: count}), nil
}

func (h *Handler) getResponse(
	ctx context.Context,
	key string,
	wrap func(*v1.Url) *connect.Response[v1.CreateResponse],
) (*connect.Response[v1.CreateResponse], error) {
	url, err := h.url(ctx, key)
	if err != nil {
		return nil, err
	}
	return wrap(url), nil
}

func (h *Handler) url(ctx context.Context, key string) (*v1.Url, error) {
	info, err := h.store.GetURL(ctx, key)
	if err != nil {
		return nil, storeError("read", err)
	}
	count, err := h.store.GetClickCount(ctx, key)
	if err != nil {
		return nil, storeError("read the count", err)
	}
	return &v1.Url{
		Key:        info.URLKey,
		LongUrl:    info.LongURL,
		CreatedAt:  timestamppb.New(info.CreatedAt),
		UpdatedAt:  optionalStamp(info.UpdatedAt),
		DeletedAt:  optionalStamp(info.DeletedAt),
		ExpiresAt:  optionalStamp(info.ExpiresAt),
		ClickCount: count,
	}, nil
}

// validKey refuses a key the column cannot hold.
//
// Here rather than at the database, because a constraint violation reaches a
// caller as "internal" and tells them nothing they can act on.
func validKey(key string) error {
	if len(key) != KeyLength {
		return connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("key must be exactly %d characters, got %d", KeyLength, len(key)))
	}
	return nil
}

// storeError turns a storage failure into a code a client can act on.
//
// Only "not found" is distinguished, and deliberately: every other failure is
// this service's problem, and a caller that could tell a constraint violation
// from a dropped connection would start depending on which it got.
func storeError(what string, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return connect.NewError(connect.CodeNotFound, errors.New("no such key"))
	}
	return connect.NewError(connect.CodeInternal, fmt.Errorf("%s: %w", what, err))
}

func optionalTime(stamp *timestamppb.Timestamp) *time.Time {
	if stamp == nil {
		return nil
	}
	value := stamp.AsTime()
	return &value
}

func optionalStamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}
