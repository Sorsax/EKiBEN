package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"database/sql"

	"ekiben-agent/internal/config"
	"ekiben-agent/internal/db"
	"ekiben-agent/internal/logger"
	"ekiben-agent/internal/protocol"
	"ekiben-agent/internal/version"

	"github.com/gorilla/websocket"
)

type Agent struct {
	cfg    config.Config
	db     *sql.DB
	api    *db.APIClient
	logger *logger.Logger

	connMu    sync.Mutex
	conn      *websocket.Conn
	inflight  sync.WaitGroup
	shutdown  atomic.Bool
}

func New(cfg config.Config, sqlDB *sql.DB, apiClient *db.APIClient, log *logger.Logger) *Agent {
	return &Agent{cfg: cfg, db: sqlDB, api: apiClient, logger: log}
}

// BeginShutdown signals the agent to stop accepting new work and close connections.
func (a *Agent) BeginShutdown() {
	a.shutdown.Store(true)
	a.closeConn()
}

// WaitForInflight waits for in-flight work to finish up to the timeout.
func (a *Agent) WaitForInflight(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		a.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
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

		err := a.connectOnce(ctx)
		if err != nil {
			a.logger.Warnf("Connection failed: %v", err)
		}
		
		// Only log if we're reconnecting
		a.logger.Infof("Reconnecting in %s...", a.cfg.ReconnectDelay)

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
	a.setConn(conn)
	defer a.clearConn(conn)

	register := protocol.Envelope{
		Type:    "register",
		AgentID: a.cfg.AgentID,
		Version: version.Version,
		Meta: map[string]any{
			"allowWrite": a.cfg.AllowWrite,
			"source":     a.cfg.SourceMode,
			"dbPath":     a.cfg.DBPath,
			"apiBaseUrl": a.cfg.APIBaseURL,
		},
	}
	a.logger.TrafficTx("register", register)
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

	connected := false
	controllerType := "Controller"
	if strings.Contains(a.cfg.ControllerURL, "jido.sorsax.dev") {
		controllerType = "Jidotachi"
	}
	
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
			if a.shutdown.Load() {
				return nil
			}
			
			// Log successful connection on first message
			if !connected {
				a.logger.Infof("Connected to %s successfully", controllerType)
				a.logActiveEvents()
				a.logger.Infof("Checking for custom songs")
				time.Sleep(500 * time.Millisecond)
				a.logger.Infof("%s Custom songs found", a.logger.Accent("0"))
				connected = true
			}
			
			a.logger.TrafficRx("message", msg.data)
			a.inflight.Add(1)
			msgCtx := ctx
			if a.shutdown.Load() {
				msgCtx = context.Background()
			}
			if err := a.handleMessage(msgCtx, conn, msg.data); err != nil {
				a.logger.Errorf("handle message: %v", err)
			}
			a.inflight.Done()
		}
	}
}

func (a *Agent) setConn(conn *websocket.Conn) {
	a.connMu.Lock()
	a.conn = conn
	a.connMu.Unlock()
}

func (a *Agent) clearConn(conn *websocket.Conn) {
	a.connMu.Lock()
	if a.conn == conn {
		a.conn = nil
	}
	a.connMu.Unlock()
}

func (a *Agent) closeConn() {
	a.connMu.Lock()
	if a.conn != nil {
		_ = a.conn.Close()
	}
	a.connMu.Unlock()
}

type eventFolderEntry struct {
	FolderID int `json:"folderId"`
}

func (a *Agent) logActiveEvents() {
	if a.cfg.DBPath == "" {
		return
	}

	baseDir := filepath.Dir(a.cfg.DBPath)
	filePath := filepath.Join(baseDir, "data", "event_folder_data.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		a.logger.Warnf("Could not load active events")
		return
	}

	var entries []eventFolderEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		a.logger.Warnf("Could not load active events")
		return
	}

	ids := make([]int, 0, len(entries))
	seen := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if _, ok := seen[entry.FolderID]; ok {
			continue
		}
		seen[entry.FolderID] = struct{}{}
		ids = append(ids, entry.FolderID)
	}
	sort.Ints(ids)

	if len(ids) == 0 {
		a.logger.Infof("Events active: none")
		return
	}

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, a.logger.Accent(strconv.Itoa(id)))
	}

	a.logger.Infof("Events active: %s", strings.Join(parts, ", "))
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
		resp.Result = map[string]any{"pong": true, "version": version.Version}
	case "version.get":
		resp.Result = map[string]any{"version": version.Version}
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

	a.logger.TrafficTx("response", resp)
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
