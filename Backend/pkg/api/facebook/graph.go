package facebook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Graph is the part of the Graph API the connector uses. Client talks to
// graph.facebook.com; FakeGraph (fake.go) stands in for it in tests.
type Graph interface {
	// ExchangeCode trades the Login dialog's code for a short-lived user token.
	ExchangeCode(ctx context.Context, code, redirectURI string) (Token, error)
	// LongLived trades a short-lived user token for a long-lived one (about 60 days).
	LongLived(ctx context.Context, token string) (Token, error)
	// Me reads the person's id and name, and their city when withLocation.
	Me(ctx context.Context, token string, withLocation bool) (Profile, error)
	// Permissions lists what the person granted or declined.
	Permissions(ctx context.Context, token string) ([]Permission, error)
	// Likes reads liked Pages, following the paging, up to limit Pages.
	Likes(ctx context.Context, token string, limit int) ([]Like, error)
	// Friends reads the friends who also use the app, up to limit.
	Friends(ctx context.Context, token string, limit int) ([]Friend, error)
	// Revoke removes the app's permissions (DELETE /me/permissions).
	Revoke(ctx context.Context, token string) error
}

// Token is a user access token and its lifetime (0 = not stated).
type Token struct {
	AccessToken string
	ExpiresIn   time.Duration
}

// Profile is /me: id, name and the city name when shared ("Atlanta, Georgia").
type Profile struct {
	ID       string
	Name     string
	Location string
}

// Permission is one row of /me/permissions (status granted | declined | expired).
type Permission struct {
	Name   string
	Status string
}

// Like is one liked Page.
type Like struct {
	ID         string
	Name       string
	Category   string
	Categories []string // category_list names
	LikedAt    *time.Time
}

// Friend is a friend who also uses the app.
type Friend struct {
	ID   string
	Name string
}

// GraphError is an error answer from the Graph API
// ({"error": {"message", "type", "code", "error_subcode", "fbtrace_id"}}).
type GraphError struct {
	HTTPStatus int
	Code       int
	Subcode    int
	Type       string
	Message    string
	TraceID    string
}

func (e *GraphError) Error() string {
	return fmt.Sprintf("graph error %d/%d (%s, HTTP %d): %s", e.Code, e.Subcode, e.Type, e.HTTPStatus, e.Message)
}

// TokenRejected reports whether Facebook no longer accepts the user token:
// code 190 (expired, revoked, password changed) or 102 (session invalid).
func TokenRejected(err error) bool {
	var ge *GraphError
	return errors.As(err, &ge) && (ge.Code == 190 || ge.Code == 102)
}

// RateLimited reports Graph throttling (codes 4, 17, 32, 613).
func RateLimited(err error) bool {
	var ge *GraphError
	if !errors.As(err, &ge) {
		return false
	}
	switch ge.Code {
	case 4, 17, 32, 613:
		return true
	}
	return false
}

// PermissionMissing reports an edge the person did not grant (code 10 or 200–299).
func PermissionMissing(err error) bool {
	var ge *GraphError
	return errors.As(err, &ge) && (ge.Code == 10 || (ge.Code >= 200 && ge.Code <= 299))
}

// Defaults of the real client.
const (
	GraphBaseURL    = "https://graph.facebook.com"
	graphPageSize   = 100
	graphTimeout    = 15 * time.Second
	maxGraphBody    = 4 << 20
	defaultTokenTTL = 60 * 24 * time.Hour
)

// Client is the real Graph API client. Every call made with a user token
// carries appsecret_proof (HMAC-SHA256 of the token keyed with the app
// secret); the OAuth token endpoints authenticate with client_secret instead.
// Errors never include request URLs, which carry the token or the secret.
type Client struct {
	AppID     string
	AppSecret string
	Version   string       // "v26.0"
	BaseURL   string       // GraphBaseURL when empty
	HTTP      *http.Client // a 15 s client when nil
}

// NewClient is the production client (nil without an app id and secret).
func NewClient(appID, appSecret, version string) *Client {
	if appID == "" || appSecret == "" {
		return nil
	}
	return &Client{AppID: appID, AppSecret: appSecret, Version: version, HTTP: &http.Client{Timeout: graphTimeout}}
}

// Proof is the appsecret_proof of a token.
func Proof(appSecret, token string) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return GraphBaseURL
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: graphTimeout}
}

// call sends one request to /<version><path> and decodes a 2xx JSON answer
// into out; a user token adds access_token and appsecret_proof.
func (c *Client) call(ctx context.Context, method, path string, params url.Values, token string, out any) error {
	query := url.Values{}
	for k, v := range params {
		query[k] = append([]string(nil), v...)
	}
	if token != "" {
		query.Set("access_token", token)
		query.Set("appsecret_proof", Proof(c.AppSecret, token))
	}
	endpoint := c.base() + "/" + c.Version + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return fmt.Errorf("graph %s %s: bad request", method, path)
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.httpClient().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL carries the token or the app secret
		}
		return fmt.Errorf("graph %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxGraphBody))
	if err != nil {
		return fmt.Errorf("graph %s %s: read: %w", method, path, err)
	}
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    int    `json:"code"`
			Subcode int    `json:"error_subcode"`
			TraceID string `json:"fbtrace_id"`
		} `json:"error"`
	}
	// A body that is not JSON falls through to the status check.
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		e := envelope.Error
		return &GraphError{HTTPStatus: res.StatusCode, Code: e.Code, Subcode: e.Subcode, Type: e.Type, Message: e.Message, TraceID: e.TraceID}
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &GraphError{HTTPStatus: res.StatusCode, Message: "unexpected response"}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("graph %s %s: decode: %w", method, path, err)
	}
	return nil
}

type tokenAnswer struct {
	AccessToken string          `json:"access_token"`
	ExpiresIn   json.RawMessage `json:"expires_in"`
}

func (t tokenAnswer) token() (Token, error) {
	if t.AccessToken == "" {
		return Token{}, &GraphError{Message: "no access_token in the answer"}
	}
	out := Token{AccessToken: t.AccessToken}
	// expires_in is a number of seconds, sometimes sent as a string.
	if secs, err := strconv.ParseInt(strings.Trim(string(t.ExpiresIn), `"`), 10, 64); err == nil && secs > 0 {
		out.ExpiresIn = time.Duration(secs) * time.Second
	}
	return out, nil
}

func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI string) (Token, error) {
	var ans tokenAnswer
	params := url.Values{"client_id": {c.AppID}, "client_secret": {c.AppSecret}, "redirect_uri": {redirectURI}, "code": {code}}
	if err := c.call(ctx, http.MethodGet, "/oauth/access_token", params, "", &ans); err != nil {
		return Token{}, err
	}
	return ans.token()
}

func (c *Client) LongLived(ctx context.Context, token string) (Token, error) {
	var ans tokenAnswer
	params := url.Values{"grant_type": {"fb_exchange_token"}, "client_id": {c.AppID}, "client_secret": {c.AppSecret},
		"fb_exchange_token": {token}}
	if err := c.call(ctx, http.MethodGet, "/oauth/access_token", params, "", &ans); err != nil {
		return Token{}, err
	}
	return ans.token()
}

func (c *Client) Me(ctx context.Context, token string, withLocation bool) (Profile, error) {
	fields := "id,name"
	if withLocation {
		fields += ",location"
	}
	var ans struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Location *struct {
			Name string `json:"name"`
		} `json:"location"`
	}
	if err := c.call(ctx, http.MethodGet, "/me", url.Values{"fields": {fields}}, token, &ans); err != nil {
		return Profile{}, err
	}
	if ans.ID == "" {
		return Profile{}, &GraphError{Message: "no id in /me"}
	}
	p := Profile{ID: ans.ID, Name: ans.Name}
	if ans.Location != nil {
		p.Location = strings.TrimSpace(ans.Location.Name)
	}
	return p, nil
}

func (c *Client) Permissions(ctx context.Context, token string) ([]Permission, error) {
	var ans struct {
		Data []struct {
			Permission string `json:"permission"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, "/me/permissions", nil, token, &ans); err != nil {
		return nil, err
	}
	out := make([]Permission, 0, len(ans.Data))
	for _, row := range ans.Data {
		out = append(out, Permission{Name: row.Permission, Status: row.Status})
	}
	return out, nil
}

// paging is the Graph list envelope's paging block.
type paging struct {
	Next string `json:"next"`
}

// pages follows a list edge: each answer's paging.next supplies the
// parameters of the next request (cursors or offsets; its token is ignored
// and the host is never contacted directly), until limit items, the last
// page, or an empty page. each consumes one page's data and returns how many
// rows it read (0 stops).
func (c *Client) pages(ctx context.Context, path string, params url.Values, token string, limit int, each func(json.RawMessage) int) error {
	requests := limit/graphPageSize + 2
	for range requests {
		var ans struct {
			Data   json.RawMessage `json:"data"`
			Paging paging          `json:"paging"`
		}
		if err := c.call(ctx, http.MethodGet, path, params, token, &ans); err != nil {
			return err
		}
		n := each(ans.Data)
		if n <= 0 || ans.Paging.Next == "" {
			return nil
		}
		next, err := url.Parse(ans.Paging.Next)
		if err != nil {
			return nil
		}
		fields, limit := params.Get("fields"), params.Get("limit")
		params = next.Query()
		params.Del("access_token")
		params.Del("appsecret_proof")
		if params.Get("fields") == "" {
			params.Set("fields", fields)
		}
		if params.Get("limit") == "" {
			params.Set("limit", limit)
		}
	}
	return nil
}

// graphTime parses Graph's "2019-05-03T12:00:00+0000" (or RFC 3339).
func graphTime(s string) *time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05-0700", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func (c *Client) Likes(ctx context.Context, token string, limit int) ([]Like, error) {
	var out []Like
	seen := map[string]bool{}
	params := url.Values{"fields": {"id,name,category,category_list,created_time"}, "limit": {strconv.Itoa(graphPageSize)}}
	err := c.pages(ctx, "/me/likes", params, token, limit, func(raw json.RawMessage) int {
		var rows []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Category     string `json:"category"`
			CategoryList []struct {
				Name string `json:"name"`
			} `json:"category_list"`
			CreatedTime string `json:"created_time"`
		}
		if json.Unmarshal(raw, &rows) != nil {
			return 0
		}
		for _, row := range rows {
			if row.ID == "" || seen[row.ID] || len(out) >= limit {
				continue
			}
			seen[row.ID] = true
			like := Like{ID: row.ID, Name: row.Name, Category: row.Category, LikedAt: graphTime(row.CreatedTime)}
			for _, cat := range row.CategoryList {
				if cat.Name != "" {
					like.Categories = append(like.Categories, cat.Name)
				}
			}
			out = append(out, like)
		}
		if len(out) >= limit {
			return 0
		}
		return len(rows)
	})
	return out, err
}

func (c *Client) Friends(ctx context.Context, token string, limit int) ([]Friend, error) {
	var out []Friend
	seen := map[string]bool{}
	params := url.Values{"fields": {"id,name"}, "limit": {strconv.Itoa(graphPageSize)}}
	err := c.pages(ctx, "/me/friends", params, token, limit, func(raw json.RawMessage) int {
		var rows []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &rows) != nil {
			return 0
		}
		for _, row := range rows {
			if row.ID == "" || seen[row.ID] || len(out) >= limit {
				continue
			}
			seen[row.ID] = true
			out = append(out, Friend{ID: row.ID, Name: row.Name})
		}
		if len(out) >= limit {
			return 0
		}
		return len(rows)
	})
	return out, err
}

func (c *Client) Revoke(ctx context.Context, token string) error {
	return c.call(ctx, http.MethodDelete, "/me/permissions", nil, token, nil)
}
