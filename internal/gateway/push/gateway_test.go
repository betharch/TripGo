package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/betharch/TripGo/internal/gateway/push"
)

var (
	tripID   = uuid.MustParse("1f0a9c62-4a1c-4f2e-9d33-2a4bb0f0b111")
	driverID = uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5")
)

type contractRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID string            `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Data        map[string]string `json:"data"`
}

type received struct {
	method      string
	path        string
	contentType string
	body        contractRequest
	decodeErr   error
}

type closeCounter struct {
	base   http.RoundTripper
	opened atomic.Int32
	closed atomic.Int32
}

type countedBody struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (b countedBody) Close() error {
	b.closed.Add(1)
	return b.ReadCloser.Close()
}

func (c *closeCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	c.opened.Add(1)
	resp.Body = countedBody{ReadCloser: resp.Body, closed: &c.closed}
	return resp, nil
}

func newGateway(t *testing.T, handler http.HandlerFunc) (*push.Gateway, *closeCounter) {
	t.Helper()
	srv := httptest.NewServer(handler)
	transport := &http.Transport{}
	counter := &closeCounter{base: transport}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		srv.Close()
	})
	gateway, err := push.New(srv.URL, &http.Client{Transport: counter})
	require.NoError(t, err)
	return gateway, counter
}

func respond(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message_id":"m-1","accepted_at":"2026-11-20T12:00:00Z"}`))
	}
}

func TestRequestPositionSendsContractRequest(t *testing.T) {
	got := make(chan received, 1)
	gateway, _ := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		rec := received{method: r.Method, path: r.URL.Path, contentType: r.Header.Get("Content-Type")}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		rec.decodeErr = dec.Decode(&rec.body)
		got <- rec
		w.WriteHeader(http.StatusAccepted)
	})

	err := gateway.RequestPosition(t.Context(), tripID, driverID)

	require.NoError(t, err)
	rec := <-got
	assert.Equal(t, http.MethodPost, rec.method)
	assert.Equal(t, "/api/v1/push", rec.path)
	assert.Equal(t, "application/json", rec.contentType)
	require.NoError(t, rec.decodeErr)
	assert.NoError(t, uuid.Validate(rec.body.RequestID))
	assert.Equal(t, contractRequest{
		RequestID:   rec.body.RequestID,
		RecipientID: driverID.String(),
		Kind:        "REQUEST_POSITION",
		Data:        map[string]string{"trip_id": tripID.String()},
	}, rec.body)
}

func TestRequestPositionUsesNewRequestIDForEveryAttempt(t *testing.T) {
	ids := make(chan string, 2)
	gateway, _ := newGateway(t, func(w http.ResponseWriter, r *http.Request) {
		var body contractRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		ids <- body.RequestID
		w.WriteHeader(http.StatusAccepted)
	})

	require.NoError(t, gateway.RequestPosition(t.Context(), tripID, driverID))
	require.NoError(t, gateway.RequestPosition(t.Context(), tripID, driverID))

	assert.NotEqual(t, <-ids, <-ids)
}

func TestRequestPositionClassifiesResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "accepted", status: http.StatusAccepted},
		{name: "bad request", status: http.StatusBadRequest, want: push.ErrRejected},
		{name: "not found", status: http.StatusNotFound, want: push.ErrRejected},
		{name: "rate limited", status: http.StatusTooManyRequests, want: push.ErrUnavailable},
		{name: "internal error", status: http.StatusInternalServerError, want: push.ErrUnavailable},
		{name: "unavailable", status: http.StatusServiceUnavailable, want: push.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, counter := newGateway(t, respond(tt.status))

			err := gateway.RequestPosition(t.Context(), tripID, driverID)

			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
				assert.NotErrorIs(t, err, push.ErrTransport)
			}
			assert.Equal(t, int32(1), counter.opened.Load())
			assert.Equal(t, int32(1), counter.closed.Load())
		})
	}
}

func TestRequestPositionStopsAtContextDeadline(t *testing.T) {
	release := make(chan struct{})
	gateway, _ := newGateway(t, func(http.ResponseWriter, *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	err := gateway.RequestPosition(ctx, tripID, driverID)

	require.ErrorIs(t, err, push.ErrTransport)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRequestPositionReportsUnreachableService(t *testing.T) {
	refused := errors.New("connect: connection refused")
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, refused
	}}
	gateway, err := push.New("http://push.invalid", &http.Client{Transport: transport})
	require.NoError(t, err)

	err = gateway.RequestPosition(t.Context(), tripID, driverID)

	require.ErrorIs(t, err, push.ErrTransport)
	require.ErrorIs(t, err, refused)
	assert.NotErrorIs(t, err, push.ErrUnavailable)
}

func TestNewRejectsBrokenURL(t *testing.T) {
	_, err := push.New("http://local host", &http.Client{})

	require.Error(t, err)
}
