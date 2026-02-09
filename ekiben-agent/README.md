# ekiben-agent

A small Windows-friendly agent that connects outbound to your controller (Node server) and provides safe, named SQLite queries against `taiko.db3`.

This avoids port forwarding because the agent always opens the connection first and keeps it open.

## What it does
- Opens a persistent WebSocket connection to the controller.
- Registers itself and waits for request messages.
- Executes allowlisted named queries against SQLite.
- Returns JSON results for reads and write metadata for writes.

## Quick start (end-to-end)
1) Configure the agent in `agent.config.psd1`.
2) Start your Node controller with a WebSocket endpoint (see "Node controller setup").
3) Install the agent as a Windows service with `install-service.ps1`.
4) Call your controller API to issue queries to the agent.

## Requirements
- Go 1.22+ to build.
- Network access from the DB host to your controller URL.
- Read access to `taiko.db3` (and write access if `--allow-write` is enabled).

## Build

```powershell
cd ekiben-agent

go build -o ekiben-agent.exe ./cmd/agent
```

## Run

```powershell
./ekiben-agent.exe `
  --controller wss://your-controller.example/ws `
  --token YOUR_AGENT_TOKEN `
  --agent-id agent-001 `
  --db D:\Webbivelhoilut\EKiBEN\taiko.db3
```

Optional write support:

```powershell
./ekiben-agent.exe --allow-write ...
```

## Run as a Windows service

1) Edit the config file:

- `agent.config.psd1` (in the same folder as the agent exe)

2) Run the install script (PowerShell as Administrator):

```powershell
cd ekiben-agent

./install-service.ps1
```

Optional write support:

```powershell
./install-service.ps1 -AllowWrite
```

## Agent configuration file
Edit `agent.config.psd1` before installing the service. Fields:
- `ServiceName`: Windows service name (default: EkibenAgent).
- `Controller`: WebSocket URL the agent connects to (wss://.../ws).
- `Token`: Secret used to authenticate the agent to the controller.
- `AgentId`: Unique ID for this agent (used by controller routing).
- `DbPath`: Full path to `taiko.db3`.
- `AllowWrite`: Enable write queries (default false).
- `Ping`: WebSocket ping interval (e.g., 20s).
- `Reconnect`: Reconnect delay (e.g., 5s).
- `Timeout`: Per-request timeout (e.g., 10s).

## Local DB check (runner)

```powershell
cd ekiben-agent

go run ./cmd/dbcheck --db D:\Webbivelhoilut\EKiBEN\taiko.db3
```

## Protocol (simple)
### WebSocket endpoint
- Controller exposes a WebSocket endpoint, for example: `wss://your-controller.example/ws`.
- Agent connects with headers:
  - `Authorization: Bearer <Token>`
  - `X-Agent-Id: <AgentId>`

### Register (agent -> controller)
On connect, the agent sends:

```json
{
  "type": "register",
  "agentId": "agent-001",
  "version": "0.1.0",
  "meta": {
    "allowWrite": false,
    "dbPath": "D:\\Webbivelhoilut\\EKiBEN\\taiko.db3"
  }
}
```

### Query (controller -> agent)
The controller sends a JSON message:

```json
{
  "id": "req-123",
  "method": "query",
  "params": {
    "name": "get_user_by_baid",
    "args": [17]
  }
}
```

### Response (agent -> controller)
Agent replies:

```json
{
  "type": "response",
  "id": "req-123",
  "result": {
    "rows": [
      {"Baid":17,"MyDonName":"Yomogimaru"}
    ]
  }
}
```

### Table CRUD (controller -> agent)
These methods allow full read/write access to all tables (with validation against known columns).

Select:

```json
{
  "id": "req-200",
  "method": "table.select",
  "params": {
    "table": "UserData",
    "filters": {"Baid": 17},
    "limit": 1
  }
}
```

Insert:

```json
{
  "id": "req-201",
  "method": "table.insert",
  "params": {
    "table": "Card",
    "values": {"AccessCode": "52027710920250855654", "Baid": 1}
  }
}
```

Update:

```json
{
  "id": "req-202",
  "method": "table.update",
  "params": {
    "table": "UserData",
    "values": {"MyDonName": "DON-chan"},
    "filters": {"Baid": 1}
  }
}
```

Delete:

```json
{
  "id": "req-203",
  "method": "table.delete",
  "params": {
    "table": "Tokens",
    "filters": {"Baid": 1, "Id": 100100}
  }
}
```

### Ping (controller -> agent)
Optional keepalive:

```json
{ "id": "ping-1", "method": "ping" }
```

## Named queries
Defined in `internal/db/db.go`:
- `get_user_by_baid`
- `list_cards`
- `list_song_best_by_baid`
- `update_user_name` (write)

To add more endpoints, add a new named query and call it from the controller.

## Supported tables and columns
Validated using the CSV headers from `exported_all_db`:

Tables:
- `UserData`
- `Credential`
- `Card`
- `Tokens`
- `SongPlayData`
- `SongBestData`
- `AiScoreData`
- `AiSectionScoreData`
- `DanScoreData`
- `DanStageScoreData`
- `sqlite_sequence`
- `__EFMigrationsHistory`

## Node controller setup (minimum)
You need a WebSocket endpoint that accepts agent connections and a REST endpoint that your app calls.

### WebSocket behavior (agent channel)
1) Accept WebSocket connection.
2) Read the `register` message and store the socket by `agentId`.
3) When your REST API needs data, send a `query` message over that socket.
4) Match responses by `id` and return the result to the REST caller.

### Suggested REST endpoints (controller)
- `POST /api/agents/{agentId}/query`
  - Body: `{ "name": "get_user_by_baid", "args": [17] }`
  - Response: `{ "rows": [...] }` or `{ "error": ... }`
- `GET /api/agents` (list connected agents)

### Minimal Node example (pseudo-code)
```js
// ws = new WebSocketServer({ path: "/ws" })
ws.on("connection", (socket, req) => {
  // read headers for auth (Authorization, X-Agent-Id)
  socket.on("message", (data) => {
    const msg = JSON.parse(data);
    if (msg.type === "register") {
      agents.set(msg.agentId, socket);
      return;
    }
    // responses: route by msg.id
  });
});

// REST: POST /api/agents/:agentId/query
// 1) build { id, method:"query", params:{name,args} }
// 2) send over WebSocket to that agent
// 3) wait for matching response id
// 4) return result
```

## How the full flow works
1) Agent connects outbound to the controller (no port forwarding).
2) Controller registers the agent and keeps the socket open.
3) Your app calls controller REST endpoints to fetch or update data.
4) Controller relays a named query to the agent.
5) Agent executes locally on SQLite and returns JSON.

## Security notes
- Always use `wss://` (TLS) and validate tokens.
- Do not allow arbitrary SQL from the controller; only use named queries.
- Keep `AllowWrite` off unless needed.

## Troubleshooting
- If no data returns, verify the controller URL and firewall allows outbound.
- If you see "write queries disabled", set `AllowWrite = $true` in config.
- Check Windows Service status: `sc.exe query EkibenAgent`.
- If the controller restarts, the agent retries automatically every `Reconnect` interval.
