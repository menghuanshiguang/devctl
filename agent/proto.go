package main

type Msg struct {
	T       string   `json:"t"`
	ID      int64    `json:"id,omitempty"`
	Method  string   `json:"method,omitempty"`
	Args    []string `json:"args,omitempty"`
	Data    string   `json:"data,omitempty"`
	Token   string   `json:"token,omitempty"`
	Name    string   `json:"name,omitempty"`
	Ok      *bool    `json:"ok,omitempty"`
	RC      int      `json:"rc,omitempty"`
	Stdout  string   `json:"stdout,omitempty"`
	Stderr  string   `json:"stderr,omitempty"`
	Ev      string   `json:"ev,omitempty"`
	Version string   `json:"version,omitempty"`
	Device  string   `json:"device,omitempty"`
}

const version = "v0.2"

func boolp(b bool) *bool { return &b }
