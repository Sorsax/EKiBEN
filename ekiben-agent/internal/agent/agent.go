package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"database/sql"

	"ekiben-agent/internal/config"
	"ekiben-agent/internal/db"
	"ekiben-agent/internal/protocol"

	"github.com/gorilla/websocket"
)

const version = "0.1.1"

type Agent struct {
	cfg    config.Config
	db     *sql.DB
	api    *db.APIClient
	logger *log.Logger
}

func New(cfg config.Config, sqlDB *sql.DB, apiClient *db.APIClient, logger *log.Logger) *Agent {
	return &Agent{cfg: cfg, db: sqlDB, api: apiClient, logger: logger}
}

func (a *Agent) Run(ctx context.Context) error {
	if a.cfg.ControllerURL == "" || a.cfg.Token == "" || a.cfg.AgentID == "" {
		return errors.New("missing controller, token, or agent-id")
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		a.logger.Printf("connecting to %s", a.cfg.ControllerURL)
		err := a.connectOnce(ctx)
		if err != nil {
			a.logger.Printf("connection ended: %v", err)
		}
		a.logger.Printf("reconnecting in %s", a.cfg.ReconnectDelay)

		timer := time.NewTimer(a.cfg.ReconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (a *Agent) connectOnce(ctx context.Context) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+a.cfg.Token)
	headers.Set("X-Agent-Id", a.cfg.AgentID)

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, a.cfg.ControllerURL, headers)
	if err != nil {
		return err
	}
	defer conn.Close()

	register := protocol.Envelope{
		Type:    "register",
		AgentID: a.cfg.AgentID,
		Version: version,
		Meta: map[string]any{
			"allowWrite": a.cfg.AllowWrite,
			"source":     a.cfg.SourceMode,
			"dbPath":     a.cfg.DBPath,
			"apiBaseUrl": a.cfg.APIBaseURL,
		},
	}
	if a.cfg.LogTraffic {
		a.logJSON("tx register", register)
	}
	if err := conn.WriteJSON(register); err != nil {
		return err
	}

	pingTicker := time.NewTicker(a.cfg.PingInterval)
	defer pingTicker.Stop()

	readCh := make(chan readResult, 1)
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()

	conn.SetReadDeadline(time.Now().Add(a.cfg.PingInterval * 2))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(a.cfg.PingInterval * 2))
		return nil
	})

	go a.readLoop(readCtx, conn, readCh)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pingTicker.C:
			_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(3*time.Second))
		case msg := <-readCh:
			if msg.err != nil {
				return msg.err
			}
			if a.cfg.LogTraffic {
				a.logRaw("rx", msg.data)
			}
			if err := a.handleMessage(ctx, conn, msg.data); err != nil {
				a.logger.Printf("handle message: %v", err)
			}
		}
	}
}

type readResult struct {
	data []byte
	err  error
}

func (a *Agent) readLoop(ctx context.Context, conn *websocket.Conn, out chan<- readResult) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			out <- readResult{err: err}
			return
		}
		out <- readResult{data: data}
	}
}

func (a *Agent) handleMessage(ctx context.Context, conn *websocket.Conn, data []byte) error {
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}

	if env.Method == "" {
		return nil
	}

	resp := protocol.Envelope{Type: "response", ID: env.ID}

	switch env.Method {
	case "ping":
		resp.Result = map[string]any{"pong": true, "version": version}
	case "version.get":
		resp.Result = map[string]any{"version": version}
	case "query":
		var params protocol.QueryParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		ctxTimeout, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
		defer cancel()

		result, err := a.queryNamed(ctxTimeout, params.Name, params.Args)
		if err != nil {
			resp.Error = &protocol.Error{Code: "db_error", Message: err.Error()}
			break
		}
		resp.Result = result
	case "table.select":
		var params protocol.TableSelectParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		ctxTimeout, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
		defer cancel()

		orderBy := make([]db.OrderBy, 0, len(params.OrderBy))
		for _, item := range params.OrderBy {
			orderBy = append(orderBy, db.OrderBy{Column: item.Column, Desc: item.Desc})
		}

		result, err := a.tableSelect(ctxTimeout, params.Table, params.Columns, params.Filters, orderBy, params.Limit, params.Offset)
		if err != nil {
			resp.Error = &protocol.Error{Code: "db_error", Message: err.Error()}
			break
		}
		resp.Result = result
	case "table.insert":
		var params protocol.TableInsertParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		ctxTimeout, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
		defer cancel()

		result, err := a.tableInsert(ctxTimeout, params.Table, params.Values)
		if err != nil {
			resp.Error = &protocol.Error{Code: "db_error", Message: err.Error()}
			break
		}
		resp.Result = result
	case "table.update":
		var params protocol.TableUpdateParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		ctxTimeout, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
		defer cancel()

		result, err := a.tableUpdate(ctxTimeout, params.Table, params.Values, params.Filters)
		if err != nil {
			resp.Error = &protocol.Error{Code: "db_error", Message: err.Error()}
			break
		}
		resp.Result = result
	case "table.delete":
		var params protocol.TableDeleteParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		ctxTimeout, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
		defer cancel()

		result, err := a.tableDelete(ctxTimeout, params.Table, params.Filters)
		if err != nil {
			resp.Error = &protocol.Error{Code: "db_error", Message: err.Error()}
			break
		}
		resp.Result = result
	default:
		resp.Error = &protocol.Error{Code: "unknown_method", Message: "unsupported method"}
	}

	if a.cfg.LogTraffic {
		a.logJSON("tx response", resp)
	}
	return conn.WriteJSON(resp)
}

func (a *Agent) queryNamed(ctx context.Context, name string, args []any) (map[string]any, error) {
	if a.cfg.SourceMode == "api" {
		if a.api == nil {
			return nil, errors.New("api client is not configured")
		}
		return a.api.QueryNamed(ctx, name, args, a.cfg.AllowWrite)
	}
	if a.db == nil {
		return nil, errors.New("database is not configured")
	}
	return db.QueryNamed(ctx, a.db, name, args, a.cfg.AllowWrite)
}

func (a *Agent) tableSelect(ctx context.Context, table string, columns []string, filters map[string]any, orderBy []db.OrderBy, limit *int, offset *int) (map[string]any, error) {
	if a.cfg.SourceMode == "api" {
		if a.api == nil {
			return nil, errors.New("api client is not configured")
		}
		return a.api.TableSelect(ctx, table, columns, filters, orderBy, limit, offset)
	}
	if a.db == nil {
		return nil, errors.New("database is not configured")
	}
	return db.TableSelect(ctx, a.db, table, columns, filters, orderBy, limit, offset)
}

func (a *Agent) tableInsert(ctx context.Context, table string, values map[string]any) (map[string]any, error) {
	if a.cfg.SourceMode == "api" {
		if a.api == nil {
			return nil, errors.New("api client is not configured")
		}
		return a.api.TableInsert(ctx, table, values, a.cfg.AllowWrite)
	}
	if a.db == nil {
		return nil, errors.New("database is not configured")
	}
	return db.TableInsert(ctx, a.db, table, values, a.cfg.AllowWrite)
}

func (a *Agent) tableUpdate(ctx context.Context, table string, values map[string]any, filters map[string]any) (map[string]any, error) {
	if a.cfg.SourceMode == "api" {
		if a.api == nil {
			return nil, errors.New("api client is not configured")
		}
		return a.api.TableUpdate(ctx, table, values, filters, a.cfg.AllowWrite)
	}
	if a.db == nil {
		return nil, errors.New("database is not configured")
	}
	return db.TableUpdate(ctx, a.db, table, values, filters, a.cfg.AllowWrite)
}

func (a *Agent) tableDelete(ctx context.Context, table string, filters map[string]any) (map[string]any, error) {
	if a.cfg.SourceMode == "api" {
		if a.api == nil {
			return nil, errors.New("api client is not configured")
		}
		return a.api.TableDelete(ctx, table, filters, a.cfg.AllowWrite)
	}
	if a.db == nil {
		return nil, errors.New("database is not configured")
	}
	return db.TableDelete(ctx, a.db, table, filters, a.cfg.AllowWrite)
}

func (a *Agent) logJSON(label string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		a.logger.Printf("%s: <marshal error: %v>", label, err)
		return
	}
	a.logRaw(label, data)
}

func (a *Agent) logRaw(label string, data []byte) {
	const maxLen = 2000
	msg := string(data)
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "...<truncated>"
	}
	a.logger.Printf("%s %s", label, msg)
}
