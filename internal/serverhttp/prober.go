package serverhttp

import (
	"context"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// probeResult holds a cached Anthropic reachability result.
type probeResult struct {
	healthy bool
	at      time.Time
	err     error
}

// Prober probes the Anthropic API once per interval and caches the result.
// The daemon calls AnthropicHealthy() from /readyz without triggering a new
// API call on every request.
type Prober struct {
	client   anthropic.Client
	interval time.Duration

	mu     sync.Mutex
	result probeResult
}

// NewProber creates a Prober. interval is typically 1 hour.
func NewProber(client anthropic.Client, interval time.Duration) *Prober {
	return &Prober{
		client:   client,
		interval: interval,
	}
}

// Run starts the background probe loop. It probes immediately on start, then
// once every interval. It returns when ctx is cancelled.
func (p *Prober) Run(ctx context.Context) {
	p.probe(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probe(ctx)
		}
	}
}

// AnthropicHealthy reports whether the most recent probe succeeded.
// Returns false if no probe has been run yet.
func (p *Prober) AnthropicHealthy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result.healthy
}

func (p *Prober) probe(ctx context.Context) {
	// A lightweight probe: count tokens for a tiny string. This exercises
	// network connectivity and API key validity without consuming quota.
	_, err := p.client.Messages.CountTokens(ctx, anthropic.MessageCountTokensParams{
		Model:    anthropic.ModelClaudeSonnet4_6,
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("ping"))},
	})
	p.mu.Lock()
	p.result = probeResult{healthy: err == nil, at: time.Now(), err: err}
	p.mu.Unlock()
}
