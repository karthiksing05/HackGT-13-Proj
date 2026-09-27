// Package muse calls Muse Spark on the Meta Model API with function tools
// (POST /v1/responses). The shapes follow live responses recorded on
// 2026-09-26 (testdata/tools_*.json): a turn's output holds reasoning,
// message and function_call items; the next turn sends only the
// function_call_output items with previous_response_id.
package muse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a Muse Spark client. Its key never appears in errors or logs.
type Client struct {
	key   string
	base  string
	model string
	http  *http.Client
}

// DefaultBaseURL and DefaultModel fill in an empty base or model (MUSE_BASE_URL
// and MUSE_MODEL have the same defaults).
const (
	DefaultBaseURL = "https://api.meta.ai/v1"
	DefaultModel   = "muse-spark-1.3"
)

// New builds a client for base (DefaultBaseURL when empty) and model.
func New(key, base, model string) *Client {
	if base == "" {
		base = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{key: key, base: strings.TrimRight(base, "/"), model: model, http: &http.Client{Timeout: 90 * time.Second}}
}

// Tool is a function tool the model may call.
type Tool struct {
	Type        string         `json:"type"` // "function"
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

// Message is a conversation input item.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// FunctionCallOutput answers a function_call.
type FunctionCallOutput struct {
	Type   string `json:"type"` // "function_call_output"
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// Request is one Responses API turn.
type Request struct {
	Model              string `json:"model"`
	Instructions       string `json:"instructions,omitempty"`
	Input              []any  `json:"input"`
	Tools              []Tool `json:"tools,omitempty"`
	PreviousResponseID string `json:"previous_response_id,omitempty"`
}

// Content is a message part.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Item is one output item: reasoning, message or function_call.
type Item struct {
	Type      string    `json:"type"`
	Role      string    `json:"role,omitempty"`
	Content   []Content `json:"content,omitempty"`
	Name      string    `json:"name,omitempty"`
	CallID    string    `json:"call_id,omitempty"`
	Arguments string    `json:"arguments,omitempty"`
}

// Response is a turn's result.
type Response struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Output []Item `json:"output"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Calls are the turn's function calls, in order.
func (r *Response) Calls() []Item {
	var out []Item
	for _, it := range r.Output {
		if it.Type == "function_call" {
			out = append(out, it)
		}
	}
	return out
}

// Text is the turn's assistant text.
func (r *Response) Text() string {
	var parts []string
	for _, it := range r.Output {
		if it.Type != "message" {
			continue
		}
		for _, c := range it.Content {
			if c.Type == "output_text" && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, strings.TrimSpace(c.Text))
			}
		}
	}
	return strings.Join(parts, "\n")
}

// ErrUnavailable is Muse refusing for reasons of account or quota
// (401/402/403/429): the caller falls back rather than retrying.
var ErrUnavailable = errors.New("muse unavailable")

// Create runs one turn. The client's model fills an empty req.Model.
func (c *Client) Create(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("muse: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("muse: read: %w", err)
	}
	var out Response
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		code, msg := "", strings.TrimSpace(string(raw))
		if out.Error != nil {
			code, msg = out.Error.Code, out.Error.Message
		}
		if len(msg) > 200 {
			msg = msg[:200]
		}
		err := fmt.Errorf("muse %d %s: %s", resp.StatusCode, code, msg)
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests:
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		return nil, err
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, fmt.Errorf("muse %s: %s", out.Error.Code, out.Error.Message)
	}
	if out.ID == "" {
		return nil, errors.New("muse: response without an id")
	}
	return &out, nil
}
