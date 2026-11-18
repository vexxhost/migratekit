package openstack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type metadataRoundTripper func(*http.Request) (*http.Response, error)

func (f metadataRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestInstanceUUIDValidationAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, body          string
		status              int
		canceled, wantError bool
	}{
		{"valid", `{"uuid":"instance-id"}`, 200, false, false},
		{"missing", `{}`, 200, false, true},
		{"HTTP failure", `{"error":"unavailable"}`, 503, false, true},
		{"invalid JSON", `invalid`, 200, false, true},
		{"canceled", `{"uuid":"instance-id"}`, 200, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := http.DefaultTransport
			http.DefaultTransport = metadataRoundTripper(func(r *http.Request) (*http.Response, error) {
				if err := r.Context().Err(); err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			id, err := GetCurrentInstanceUUID(ctx)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected metadata result: id=%q err=%v", id, err)
			}
			if tc.canceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if !tc.wantError && id != "instance-id" {
				t.Fatalf("unexpected UUID %q", id)
			}
		})
	}
}
