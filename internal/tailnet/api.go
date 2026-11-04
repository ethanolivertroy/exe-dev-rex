package tailnet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// API is a minimal client for the Tailscale API, authenticated with an API
// access token (tskey-api-...). The token is only ever held in memory.
type API struct {
	Token string
	Base  string // default https://api.tailscale.com/api/v2
	HTTP  *http.Client
}

// APIError is a non-2xx response.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Tailscale API: HTTP %d: %s", e.Status, e.Message)
}

func (a *API) do(ctx context.Context, method, path, ctype string, body []byte, hdr map[string]string) (*http.Response, []byte, error) {
	base := a.Base
	if base == "" {
		base = "https://api.tailscale.com/api/v2"
	}
	hc := a.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	if resp.StatusCode/100 != 2 {
		var m struct{ Message string }
		_ = json.Unmarshal(b, &m)
		if m.Message == "" {
			m.Message = strings.TrimSpace(string(b))
		}
		return resp, b, &APIError{Status: resp.StatusCode, Message: m.Message}
	}
	return resp, b, nil
}

// GetPolicy returns the tailnet policy file as HuJSON (comments kept) and its ETag.
func (a *API) GetPolicy(ctx context.Context) ([]byte, string, error) {
	resp, b, err := a.do(ctx, "GET", "/tailnet/-/acl", "", nil, map[string]string{"Accept": "application/hujson"})
	if err != nil {
		return nil, "", err
	}
	return b, resp.Header.Get("ETag"), nil
}

// ValidatePolicy asks Tailscale to check a policy file without applying it.
func (a *API) ValidatePolicy(ctx context.Context, policy []byte) error {
	_, b, err := a.do(ctx, "POST", "/tailnet/-/acl/validate", "application/hujson", policy, nil)
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(string(b)); s != "" && s != "{}" {
		var m struct {
			Message string
			Data    []struct {
				User   string
				Errors []string
			}
		}
		if json.Unmarshal(b, &m) == nil && (m.Message != "" || len(m.Data) > 0) {
			msg := m.Message
			for _, d := range m.Data {
				msg += "; " + strings.Join(d.Errors, "; ")
			}
			return fmt.Errorf("Tailscale rejected the policy: %s", msg)
		}
	}
	return nil
}

// SetPolicy replaces the policy file, only if its ETag is still etag.
func (a *API) SetPolicy(ctx context.Context, policy []byte, etag string) error {
	_, _, err := a.do(ctx, "POST", "/tailnet/-/acl", "application/hujson", policy, map[string]string{"If-Match": etag})
	return err
}

// FederatedIdentity is a trust credential that accepts OIDC tokens.
type FederatedIdentity struct {
	ID       string   `json:"id"`
	KeyType  string   `json:"keyType"`
	Issuer   string   `json:"issuer"`
	Subject  string   `json:"subject"`
	Audience string   `json:"audience"`
	Scopes   []string `json:"scopes"`
	Tags     []string `json:"tags"`
	Revoked  string   `json:"revoked"`
}

// CreateFederatedIdentity creates a trust credential for issuer + subject.
func (a *API) CreateFederatedIdentity(ctx context.Context, description, issuer, subject string, scopes, tags []string) (*FederatedIdentity, error) {
	body, _ := json.Marshal(map[string]any{
		"keyType": "federated", "description": description,
		"issuer": issuer, "subject": subject, "scopes": scopes, "tags": tags,
	})
	_, b, err := a.do(ctx, "POST", "/tailnet/-/keys", "application/json", body, nil)
	if err != nil {
		return nil, err
	}
	var k FederatedIdentity
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, err
	}
	if k.ID == "" {
		return nil, fmt.Errorf("Tailscale API: no id in the response")
	}
	return &k, nil
}

// SetFederatedIdentityScopes replaces a trust credential's scopes, keeping
// everything else. The endpoint replaces the whole credential, so the current
// one is read first.
func (a *API) SetFederatedIdentityScopes(ctx context.Context, id string, scopes []string) error {
	_, b, err := a.do(ctx, "GET", "/tailnet/-/keys/"+url.PathEscape(id), "", nil, nil)
	if err != nil {
		return err
	}
	var k FederatedIdentity
	var full map[string]any
	if err := json.Unmarshal(b, &k); err != nil {
		return err
	}
	_ = json.Unmarshal(b, &full)
	body := map[string]any{
		"keyType": "federated", "scopes": scopes, "tags": k.Tags,
		"issuer": k.Issuer, "subject": k.Subject, "audience": k.Audience,
	}
	for _, f := range []string{"description", "customClaimRules"} {
		if v, ok := full[f]; ok && v != nil {
			body[f] = v
		}
	}
	nb, _ := json.Marshal(body)
	_, _, err = a.do(ctx, "PUT", "/tailnet/-/keys/"+url.PathEscape(id), "application/json", nb, nil)
	return err
}

// FederatedIdentity reads one trust credential.
func (a *API) FederatedIdentity(ctx context.Context, id string) (*FederatedIdentity, error) {
	_, b, err := a.do(ctx, "GET", "/tailnet/-/keys/"+url.PathEscape(id), "", nil, nil)
	if err != nil {
		return nil, err
	}
	var k FederatedIdentity
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// FederatedIdentities lists the tailnet's trust credentials. The list endpoint
// returns only key ids, so each key is read on its own.
func (a *API) FederatedIdentities(ctx context.Context) ([]FederatedIdentity, error) {
	_, b, err := a.do(ctx, "GET", "/tailnet/-/keys?all=true", "", nil, nil)
	if err != nil {
		return nil, err
	}
	var r struct{ Keys []struct{ ID string } }
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var out []FederatedIdentity
	for _, k := range r.Keys {
		f, err := a.FederatedIdentity(ctx, k.ID)
		if err != nil {
			continue // e.g. another user's key this token can't read
		}
		if f.KeyType == "federated" {
			out = append(out, *f)
		}
	}
	return out, nil
}
