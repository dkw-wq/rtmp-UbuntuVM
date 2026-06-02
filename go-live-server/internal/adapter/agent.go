package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AgentCommand is an instruction sent to a Raspberry Pi agent
// (kept as a thin adapter for future use when the server needs to push
// commands directly to agents rather than having them poll).
type AgentCommand struct {
	Action    string `json:"action"`     // start_push / stop_push
	StreamKey string `json:"stream_key"`
	PushURL   string `json:"push_url,omitempty"`
}

// AgentAPI is a client that can call out to a Raspberry Pi agent's HTTP endpoint.
type AgentAPI struct {
	http *http.Client
}

// NewAgentAPI creates a new Agent API client.
func NewAgentAPI() *AgentAPI {
	return &AgentAPI{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify sends a command to a remote agent at the given URL.
func (a *AgentAPI) Notify(agentURL string, cmd AgentCommand) error {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal agent command: %w", err)
	}

	resp, err := a.http.Post(agentURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("agent notify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("agent returned status %d", resp.StatusCode)
	}
	return nil
}
