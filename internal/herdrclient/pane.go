package herdrclient

import (
	"context"
	"fmt"
	"time"
)

const tailPollInterval = 300 * time.Millisecond

type OpenPaneOptions struct {
	Label string
	CWD   string
}

type Pane struct {
	ID          string
	WorkspaceID string
	TabID       string

	client *Client
}

func (c *Client) OpenPane(ctx context.Context, opts OpenPaneOptions) (*Pane, error) {
	params := workspaceCreateParams{CWD: opts.CWD, Label: opts.Label}

	var result workspaceCreatedResult
	if err := c.call(ctx, "workspace.create", params, &result); err != nil {
		return nil, fmt.Errorf("herdrclient: open pane: %w", err)
	}

	return &Pane{
		ID:          result.RootPane.PaneID,
		WorkspaceID: result.RootPane.WorkspaceID,
		TabID:       result.RootPane.TabID,
		client:      c,
	}, nil
}

func (p *Pane) Tail(ctx context.Context) (<-chan []byte, error) {
	out := make(chan []byte)

	go func() {
		defer close(out)

		var lastRevision uint64
		haveRevision := false
		ticker := time.NewTicker(tailPollInterval)
		defer ticker.Stop()

		for {
			var result paneReadResult
			err := p.client.call(ctx, "pane.read", paneReadParams{
				PaneID:    p.ID,
				Source:    "recent",
				StripANSI: true,
			}, &result)

			if err != nil {
				return
			}

			if !haveRevision || result.Read.Revision != lastRevision {
				haveRevision = true
				lastRevision = result.Read.Revision
				if len(result.Read.Text) > 0 {
					select {
					case out <- []byte(result.Read.Text):
					case <-ctx.Done():
						return
					}
				}
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return out, nil
}

func (p *Pane) Close(ctx context.Context) error {
	var result struct{}
	if err := p.client.call(ctx, "pane.close", paneTarget{PaneID: p.ID}, &result); err != nil {
		return fmt.Errorf("herdrclient: close pane %s: %w", p.ID, err)
	}
	return nil
}
