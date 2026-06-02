package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------- SRS HTTP API response shapes ----------

// SRSClient represents a connected RTMP client returned by the SRS API.
type SRSClient struct {
	ID     string `json:"id"`
	Vhost  string `json:"vhost"`
	Stream string `json:"stream"`
	IP     string `json:"ip"`
	Type   string `json:"type"` // "RTMP publisher" or "RTMP player"
	Alive  float64 `json:"alive"`
}

type srsClientsData struct {
	Clients []SRSClient `json:"clients"`
}

type srsResponse struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

// ---------- Client ----------

// SRSClient is the HTTP client for calling the SRS HTTP API.
type SRSAPI struct {
	baseURL string
	http    *http.Client
}

// NewSRSAPI creates a new SRS API client.
func NewSRSAPI(baseURL string) *SRSAPI {
	return &SRSAPI{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// FindPublishingClient returns the SRS client that is publishing the given stream.
// Returns nil if no matching publisher is found.
func (a *SRSAPI) FindPublishingClient(streamKey string) (*SRSClient, error) {
	// GET /api/v1/clients
	resp, err := a.http.Get(a.baseURL + "/api/v1/clients")
	if err != nil {
		return nil, fmt.Errorf("srs clients request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("srs clients read: %w", err)
	}

	var srsResp srsResponse
	if err := json.Unmarshal(body, &srsResp); err != nil {
		return nil, fmt.Errorf("srs clients parse: %w", err)
	}
	if srsResp.Code != 0 {
		return nil, fmt.Errorf("srs api error: code=%d", srsResp.Code)
	}

	var data srsClientsData
	if err := json.Unmarshal(srsResp.Data, &data); err != nil {
		return nil, fmt.Errorf("srs data parse: %w", err)
	}

	for i := range data.Clients {
		c := &data.Clients[i]
		if c.Stream == streamKey && c.Type == "RTMP publisher" {
			return c, nil
		}
	}
	return nil, nil
}

// KickClient disconnects a client by its SRS-internal client ID.
func (a *SRSAPI) KickClient(clientID string) error {
	req, err := http.NewRequest(http.MethodDelete, a.baseURL+"/api/v1/clients/"+clientID, nil)
	if err != nil {
		return fmt.Errorf("kick request: %w", err)
	}

	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("kick call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kick failed (status=%d): %s", resp.StatusCode, string(body))
	}

	// Parse response to check SRS-level code
	var srsResp srsResponse
	json.NewDecoder(resp.Body).Decode(&srsResp)
	if srsResp.Code != 0 {
		return fmt.Errorf("srs kick api error: code=%d", srsResp.Code)
	}

	return nil
}
