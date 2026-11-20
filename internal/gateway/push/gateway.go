package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

const (
	pushPath            = "api/v1/push"
	kindRequestPosition = "REQUEST_POSITION"
	maxDrainBytes       = 4 << 10
)

var (
	ErrTransport   = errors.New("push service did not respond")
	ErrUnavailable = errors.New("push service is unavailable")
	ErrRejected    = errors.New("push service rejected the request")
)

type request struct {
	RequestID   string            `json:"request_id"`
	RecipientID string            `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data"`
}

type Gateway struct {
	endpoint string
	client   *http.Client
}

func New(baseURL string, client *http.Client) (*Gateway, error) {
	endpoint, err := url.JoinPath(baseURL, pushPath)
	if err != nil {
		return nil, fmt.Errorf("build push service URL: %w", err)
	}
	return &Gateway{endpoint: endpoint, client: client}, nil
}

func (g *Gateway) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	body, err := json.Marshal(request{
		RequestID:   uuid.NewString(),
		RecipientID: driverID.String(),
		Kind:        kindRequestPosition,
		Data:        map[string]string{"trip_id": tripID.String()},
	})
	if err != nil {
		return fmt.Errorf("encode push request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTransport, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode >= http.StatusInternalServerError, resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	default:
		return fmt.Errorf("%w: status %d", ErrRejected, resp.StatusCode)
	}
}
