package herdrclient

import "encoding/json"

type wireRequest struct {
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Session string          `json:"session"`
}

type wireResponse struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wireError      `json:"error,omitempty"`
}

type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type pongResult struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

type paneInfo struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
}

type workspaceCreateParams struct {
	CWD   string `json:"cwd,omitempty"`
	Label string `json:"label,omitempty"`
	Focus bool   `json:"focus"`
}

type workspaceCreatedResult struct {
	RootPane paneInfo `json:"root_pane"`
}

type paneTarget struct {
	PaneID string `json:"pane_id"`
}

type paneReadParams struct {
	PaneID    string `json:"pane_id"`
	Source    string `json:"source"`
	StripANSI bool   `json:"strip_ansi"`
}

type paneReadResult struct {
	Read struct {
		Text     string `json:"text"`
		Revision uint64 `json:"revision"`
	} `json:"read"`
}
