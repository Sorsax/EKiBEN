package protocol

import "encoding/json"

type Envelope struct {
	Type    string          `json:"type,omitempty"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	AgentID string          `json:"agentId,omitempty"`
	Version string          `json:"version,omitempty"`
	Meta    map[string]any  `json:"meta,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type QueryParams struct {
	Name string `json:"name"`
	Args []any  `json:"args"`
}

type TableSelectParams struct {
	Table   string              `json:"table"`
	Columns []string            `json:"columns,omitempty"`
	Filters map[string]any      `json:"filters,omitempty"`
	OrderBy []TableOrderBy      `json:"orderBy,omitempty"`
	Limit   *int                `json:"limit,omitempty"`
	Offset  *int                `json:"offset,omitempty"`
}

type TableOrderBy struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc,omitempty"`
}

type TableInsertParams struct {
	Table  string         `json:"table"`
	Values map[string]any `json:"values"`
}

type TableUpdateParams struct {
	Table   string         `json:"table"`
	Values  map[string]any `json:"values"`
	Filters map[string]any `json:"filters"`
}

type TableDeleteParams struct {
	Table   string         `json:"table"`
	Filters map[string]any `json:"filters"`
}
