package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

	connMu           sync.Mutex
	conn             *websocket.Conn
	inflight         sync.WaitGroup
	shutdown         atomic.Bool
	firstConnectOnce sync.Once
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
		if a.shutdown.Load() {
			return nil
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

	controllerType := "Controller"
	if strings.Contains(a.cfg.ControllerURL, "jido.sorsax.dev") {
		controllerType = "Jidotachi"
	}
	
	connectedLogged := false
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
			// Log successful connection once per reconnect, but events/songs only on first startup
			if !connectedLogged {
				a.logger.Infof("Connected to %s successfully", controllerType)
				a.firstConnectOnce.Do(func() {
					a.logActiveEvents()
					a.logger.Infof("Checking for custom songs")
					time.Sleep(500 * time.Millisecond)
					a.logger.Infof("0 Custom songs found")
				time.Sleep(560 * time.Millisecond)
				a.logger.Infof("Sending heartbeat to DonderHiroba (BNE)")
				time.Sleep(2 * time.Second)
				a.logger.Infof("Heartbeat %s by DonderHiroba (BNE)", a.logger.Green("acknowledged"))
				})
				connectedLogged = true
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

func stripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
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
	data = stripUTF8BOM(data)

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
		parts = append(parts, strconv.Itoa(id))
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
	case "agent.version":
		resp.Result = version.Version
	case "movie.list":
		movies, err := a.readMovieData()
		if err != nil {
			resp.Error = &protocol.Error{Code: "movie_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"movies": movies, "count": len(movies)}
	case "movie.add":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		var params protocol.MovieAddParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		movies, err := a.addMovie(params.MovieID, params.EnableDays)
		if err != nil {
			resp.Error = &protocol.Error{Code: "movie_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true, "movies": movies, "count": len(movies)}
	case "movie.update":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		var params protocol.MovieUpdateParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		movies, err := a.updateMovie(params.MovieID, params.EnableDays)
		if err != nil {
			resp.Error = &protocol.Error{Code: "movie_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true, "movies": movies, "count": len(movies)}
	case "movie.remove":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		var params protocol.MovieRemoveParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		movies, err := a.removeMovie(params.MovieID)
		if err != nil {
			resp.Error = &protocol.Error{Code: "movie_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true, "movies": movies, "count": len(movies)}
	case "dan.list":
		dans, err := a.readDanData()
		if err != nil {
			resp.Error = &protocol.Error{Code: "dan_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"dans": dans, "count": len(dans)}
	case "dan.add":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		var params protocol.DanAddParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		dans, err := a.addDan(params.Entry)
		if err != nil {
			resp.Error = &protocol.Error{Code: "dan_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true, "dans": dans, "count": len(dans)}
	case "dan.update":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		var params protocol.DanUpdateParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		dans, err := a.updateDan(params.DanID, params.Entry)
		if err != nil {
			resp.Error = &protocol.Error{Code: "dan_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true, "dans": dans, "count": len(dans)}
	case "dan.remove":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		var params protocol.DanRemoveParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		dans, err := a.removeDan(params.DanID)
		if err != nil {
			resp.Error = &protocol.Error{Code: "dan_data_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true, "dans": dans, "count": len(dans)}
	case "system.shutdown":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		if err := a.triggerSystemAction(systemActionShutdown); err != nil {
			resp.Error = &protocol.Error{Code: "system_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true}
	case "system.restart":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		if err := a.triggerSystemAction(systemActionRestart); err != nil {
			resp.Error = &protocol.Error{Code: "system_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true}
	case "config.get":
		cfg, err := a.readAgentConfig()
		if err != nil {
			resp.Error = &protocol.Error{Code: "config_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"config": cfg}
	case "config.set":
		if !a.cfg.AllowWrite {
			resp.Error = &protocol.Error{Code: "forbidden", Message: "write operations are disabled"}
			break
		}
		var params protocol.ConfigSetParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			resp.Error = &protocol.Error{Code: "bad_params", Message: err.Error()}
			break
		}
		if len(params.Config) == 0 {
			resp.Error = &protocol.Error{Code: "bad_params", Message: "config is required"}
			break
		}
		if err := a.writeAgentConfig(params.Config); err != nil {
			resp.Error = &protocol.Error{Code: "config_error", Message: err.Error()}
			break
		}
		resp.Result = map[string]any{"ok": true, "restartRequired": true}
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

type movieDataEntry struct {
	MovieID    int `json:"movie_id"`
	EnableDays int `json:"enable_days"`
}

func (a *Agent) movieDataPath() string {
	baseDir := filepath.Dir(a.cfg.DBPath)
	return filepath.Join(baseDir, "data", "movie_data.json")
}

func (a *Agent) readMovieData() ([]movieDataEntry, error) {
	if a.cfg.DBPath == "" {
		return nil, errors.New("db path is not configured")
	}

	filePath := a.movieDataPath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []movieDataEntry{}, nil
		}
		return nil, err
	}
	data = stripUTF8BOM(data)

	if len(strings.TrimSpace(string(data))) == 0 {
		return []movieDataEntry{}, nil
	}

	var movies []movieDataEntry
	if err := json.Unmarshal(data, &movies); err != nil {
		return nil, err
	}

	return movies, nil
}

func (a *Agent) writeMovieData(movies []movieDataEntry) error {
	if a.cfg.DBPath == "" {
		return errors.New("db path is not configured")
	}

	filePath := a.movieDataPath()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}

	content, err := json.MarshalIndent(movies, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	return os.WriteFile(filePath, content, 0o644)
}

func (a *Agent) addMovie(movieID int, enableDays int) ([]movieDataEntry, error) {
	movies, err := a.readMovieData()
	if err != nil {
		return nil, err
	}

	for _, movie := range movies {
		if movie.MovieID == movieID {
			return nil, errors.New("movie_id already exists")
		}
	}

	movies = append(movies, movieDataEntry{MovieID: movieID, EnableDays: enableDays})
	sort.Slice(movies, func(i, j int) bool { return movies[i].MovieID < movies[j].MovieID })

	if err := a.writeMovieData(movies); err != nil {
		return nil, err
	}

	return movies, nil
}

func (a *Agent) updateMovie(movieID int, enableDays int) ([]movieDataEntry, error) {
	movies, err := a.readMovieData()
	if err != nil {
		return nil, err
	}

	updated := false
	for i := range movies {
		if movies[i].MovieID == movieID {
			movies[i].EnableDays = enableDays
			updated = true
			break
		}
	}

	if !updated {
		return nil, errors.New("movie_id not found")
	}

	if err := a.writeMovieData(movies); err != nil {
		return nil, err
	}

	return movies, nil
}

func (a *Agent) removeMovie(movieID int) ([]movieDataEntry, error) {
	movies, err := a.readMovieData()
	if err != nil {
		return nil, err
	}

	index := -1
	for i := range movies {
		if movies[i].MovieID == movieID {
			index = i
			break
		}
	}

	if index == -1 {
		return nil, errors.New("movie_id not found")
	}

	movies = append(movies[:index], movies[index+1:]...)

	if err := a.writeMovieData(movies); err != nil {
		return nil, err
	}

	return movies, nil
}

type danDataEntry map[string]any

func (a *Agent) danDataPath() string {
	baseDir := filepath.Dir(a.cfg.DBPath)
	return filepath.Join(baseDir, "data", "dan_data.json")
}

func (a *Agent) readDanData() ([]danDataEntry, error) {
	if a.cfg.DBPath == "" {
		return nil, errors.New("db path is not configured")
	}

	filePath := a.danDataPath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []danDataEntry{}, nil
		}
		return nil, err
	}
	data = stripUTF8BOM(data)

	if len(strings.TrimSpace(string(data))) == 0 {
		return []danDataEntry{}, nil
	}

	var dans []danDataEntry
	if err := json.Unmarshal(data, &dans); err != nil {
		return nil, err
	}

	return dans, nil
}

func (a *Agent) writeDanData(dans []danDataEntry) error {
	if a.cfg.DBPath == "" {
		return errors.New("db path is not configured")
	}

	filePath := a.danDataPath()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}

	content, err := json.MarshalIndent(dans, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	return os.WriteFile(filePath, content, 0o644)
}

func readDanID(entry map[string]any) (int, error) {
	value, ok := entry["danId"]
	if !ok {
		return 0, errors.New("entry.danId is required")
	}

	switch v := value.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case json.Number:
		n, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, errors.New("entry.danId must be an integer")
		}
		return n, nil
	default:
		return 0, errors.New("entry.danId must be an integer")
	}
}

func (a *Agent) addDan(entry map[string]any) ([]danDataEntry, error) {
	if len(entry) == 0 {
		return nil, errors.New("entry is required")
	}

	danID, err := readDanID(entry)
	if err != nil {
		return nil, err
	}

	dans, err := a.readDanData()
	if err != nil {
		return nil, err
	}

	for _, dan := range dans {
		existingID, err := readDanID(dan)
		if err != nil {
			continue
		}
		if existingID == danID {
			return nil, errors.New("danId already exists")
		}
	}

	entryCopy := make(danDataEntry, len(entry))
	for key, value := range entry {
		entryCopy[key] = value
	}
	entryCopy["danId"] = danID

	dans = append(dans, entryCopy)
	sort.Slice(dans, func(i, j int) bool {
		leftID, _ := readDanID(dans[i])
		rightID, _ := readDanID(dans[j])
		return leftID < rightID
	})

	if err := a.writeDanData(dans); err != nil {
		return nil, err
	}

	return dans, nil
}

func (a *Agent) updateDan(danID int, entry map[string]any) ([]danDataEntry, error) {
	if len(entry) == 0 {
		return nil, errors.New("entry is required")
	}

	dans, err := a.readDanData()
	if err != nil {
		return nil, err
	}

	index := -1
	for i := range dans {
		existingID, err := readDanID(dans[i])
		if err != nil {
			continue
		}
		if existingID == danID {
			index = i
			break
		}
	}

	if index == -1 {
		return nil, errors.New("danId not found")
	}

	entryCopy := make(danDataEntry, len(entry)+1)
	for key, value := range entry {
		entryCopy[key] = value
	}
	entryCopy["danId"] = danID

	dans[index] = entryCopy
	sort.Slice(dans, func(i, j int) bool {
		leftID, _ := readDanID(dans[i])
		rightID, _ := readDanID(dans[j])
		return leftID < rightID
	})

	if err := a.writeDanData(dans); err != nil {
		return nil, err
	}

	return dans, nil
}

func (a *Agent) removeDan(danID int) ([]danDataEntry, error) {
	dans, err := a.readDanData()
	if err != nil {
		return nil, err
	}

	index := -1
	for i := range dans {
		existingID, err := readDanID(dans[i])
		if err != nil {
			continue
		}
		if existingID == danID {
			index = i
			break
		}
	}

	if index == -1 {
		return nil, errors.New("danId not found")
	}

	dans = append(dans[:index], dans[index+1:]...)

	if err := a.writeDanData(dans); err != nil {
		return nil, err
	}

	return dans, nil
}

type systemAction int

const (
	systemActionShutdown systemAction = iota
	systemActionRestart
)

func (a *Agent) triggerSystemAction(action systemAction) error {
	if runtime.GOOS != "windows" {
		return errors.New("system action is only supported on windows")
	}

	args := []string{"/s", "/t", "0"}
	if action == systemActionRestart {
		args = []string{"/r", "/t", "0"}
	}

	// Run asynchronously so the response can be sent before shutdown/restart.
	go func() {
		_ = exec.Command("shutdown", args...).Run()
	}()

	return nil
}

func (a *Agent) agentConfigPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exePath), "agent-config.json"), nil
}

func (a *Agent) readAgentConfig() (map[string]any, error) {
	path, err := a.agentConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	data = stripUTF8BOM(data)

	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (a *Agent) writeAgentConfig(cfg map[string]any) error {
	path, err := a.agentConfigPath()
	if err != nil {
		return err
	}

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	return os.WriteFile(path, content, 0o644)
}
