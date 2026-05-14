// Package oauth implements the Google OAuth2 authorization-code flow for the
// paperclaw daemon. It provides HTTP handlers for /oauth/start and /oauth/callback,
// a store-backed TokenSource, and a DriveProvider that rebuilds the Drive client
// after a token is obtained.
package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"paper-claw/internal/store"
)

// DriveReadyFunc is called after a token is stored and the Drive client has been
// rebuilt. Typically it enqueues a scan.
type DriveReadyFunc func()

// Config holds everything needed to run the OAuth flow.
type Config struct {
	oauthCfg  *oauth2.Config
	store     *store.DB
	onReady   DriveReadyFunc
	setClient func(ctx context.Context, hc *http.Client) error

	mu     sync.Mutex
	states map[string]struct{} // pending state tokens
}

// New constructs an oauth.Config.
//   - clientID / clientSecret: Google OAuth app credentials.
//   - publicBaseURL: the daemon's public base URL (e.g. "https://example.com").
//   - redirectPath: the callback path (e.g. "/oauth/callback").
//   - st: the store used to persist/read the token.
//   - setClient: called after a token is obtained; the caller wires the Drive client.
//   - onReady: called after setClient succeeds (e.g. to enqueue a scan).
func New(
	clientID, clientSecret, publicBaseURL, redirectPath string,
	st *store.DB,
	setClient func(ctx context.Context, hc *http.Client) error,
	onReady DriveReadyFunc,
) *Config {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  publicBaseURL + redirectPath,
		Scopes:       []string{"https://www.googleapis.com/auth/drive"},
		Endpoint:     google.Endpoint,
	}
	return &Config{
		oauthCfg:  cfg,
		store:     st,
		onReady:   onReady,
		setClient: setClient,
		states:    make(map[string]struct{}),
	}
}

// TokenSource returns an oauth2.TokenSource that reads from and writes to the
// store. Refreshed tokens are automatically persisted.
func (c *Config) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	raw, err := c.store.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("oauth: reading token: %w", err)
	}
	if raw == nil {
		return nil, nil // no token yet
	}
	var tok oauth2.Token
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("oauth: unmarshalling token: %w", err)
	}
	return &storeTokenSource{
		ctx:    ctx,
		inner:  c.oauthCfg.TokenSource(ctx, &tok),
		store:  c.store,
		oauthC: c.oauthCfg,
	}, nil
}

// HTTPClient returns an *http.Client authenticated with the stored token, or
// nil if no token is stored yet.
func (c *Config) HTTPClient(ctx context.Context) (*http.Client, error) {
	ts, err := c.TokenSource(ctx)
	if err != nil || ts == nil {
		return nil, err
	}
	return oauth2.NewClient(ctx, ts), nil
}

// HandleStart handles GET /oauth/start: generates a random state, stores it,
// and redirects the user to Google's consent page.
func (c *Config) HandleStart(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	c.mu.Lock()
	c.states[state] = struct{}{}
	c.mu.Unlock()

	url := c.oauthCfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)
	http.Redirect(w, r, url, http.StatusFound)
}

// HandleCallback handles GET /oauth/callback: verifies the state, exchanges the
// code for a token, persists it, rebuilds the Drive client, and signals the
// daemon that it is now authenticated.
func (c *Config) HandleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	c.mu.Lock()
	_, valid := c.states[state]
	delete(c.states, state)
	c.mu.Unlock()
	if !valid {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	tok, err := c.oauthCfg.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, fmt.Sprintf("token exchange failed: %v", err), http.StatusBadRequest)
		return
	}

	raw, err := json.Marshal(tok) //nolint:gosec // marshaling an OAuth token is intentional
	if err != nil {
		http.Error(w, "failed to marshal token", http.StatusInternalServerError)
		return
	}
	if err := c.store.PutToken(r.Context(), raw); err != nil {
		http.Error(w, "failed to store token", http.StatusInternalServerError)
		return
	}

	ts := &storeTokenSource{
		ctx:    r.Context(),
		inner:  c.oauthCfg.TokenSource(r.Context(), tok),
		store:  c.store,
		oauthC: c.oauthCfg,
	}
	hc := oauth2.NewClient(r.Context(), ts)
	if err := c.setClient(r.Context(), hc); err != nil {
		http.Error(w, fmt.Sprintf("rebuilding Drive client: %v", err), http.StatusInternalServerError)
		return
	}

	if c.onReady != nil {
		c.onReady()
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = fmt.Fprintln(w, "paperclaw is now authenticated with Google Drive. You may close this window.")
}

// --- storeTokenSource --------------------------------------------------------

// storeTokenSource wraps an oauth2.TokenSource and persists refreshed tokens
// back to the store automatically.
type storeTokenSource struct {
	ctx    context.Context
	inner  oauth2.TokenSource
	store  *store.DB
	oauthC *oauth2.Config
}

func (s *storeTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.inner.Token()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(tok) //nolint:gosec // marshaling an OAuth token is intentional
	if err != nil {
		return tok, nil // non-fatal: best-effort persistence
	}
	_ = s.store.PutToken(s.ctx, raw)
	return tok, nil
}

// --- helpers -----------------------------------------------------------------

func randomState() (string, error) {
	b := make([]byte, 18) // 18 bytes → 24 base64 chars, no padding
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
