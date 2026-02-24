# EKiBEN

### A multi-purpose toolkit for a certain very sugoi taiko server

## This readme is outdated, please don't read it :DD

> [!WARNING]
> Only use EKiBEN where it's allowed. If you run it on a vanilla network, which you definetly shouldn't, your machine might get banned. That's on you.

## What is EKiBEN?

EKiBEN is a toolkit for making a heavily modified instance of TLS actually useful. This specific TLS version by itself is extremely barebones. It writes and reads the database, that's it.

EKiBEN runs as a standalone agent on the same machine (usually a game cabinet) as the database. This agent gives you remote database access, API access, and Jidotachi integration. If more than two machines ever run EKiBEN, it'll support cross-database or machine player data (if both operators want it). 

For copyright reasons, I will not provide any front-end assets. If you have a legitimate copy of the game, you are able to source those on your own.

## Setup

**Note:** You are expected to source your game data legally from Bandai Namco. I do not provide any files related to the game or TLS.

1. Download the EKiBEN zip file.
2. Make a new folder (for example, `ta`) on the same drive as your TLS database (`taiko.db3`).
3. Extract everything from the zip into that folder.
4. Inside, you'll see:
   - `ekiben-agent.exe` (the main program)
   - `agent-config.example.json` (example configuration file)

5. Configure the agent:
   - Copy `agent-config.example.json` to `agent-config.json` in the same directory
   - Edit `agent-config.json` with your settings:

| Setting        | Example Value                              | What it does                                      |
| :------------- | :------------------------------------------ | :------------------------------------------------ |
| controller     | wss://your-controller.example/ws/agents    | WebSocket URL of your EKiBEN controller           |
| token          | YOUR_AGENT_TOKEN                           | Authentication token for this agent               |
| agentId        | agent-001                                  | Unique name for this agent                        |
| source         | direct                                     | Data source mode: `direct` or `api`               |
| dbPath         | D:\\Path\\To\\taiko.db3                    | Full path to your TLS database file               |
| apiBaseUrl     | http://localhost:5000                      | TLS REST API base URL (required for `api` mode)   |
| apiToken       |                                            | Optional bearer token for TLS REST API            |
| allowWrite     | false                                      | true to allow remote writes                       |
| logTraffic     | false                                      | true to log all websocket traffic                 |
| pingInterval   | 20s                                        | How often to ping the controller                  |
| reconnectDelay | 5s                                         | Wait time before reconnecting                     |
| requestTimeout | 10s                                        | Request timeout                                   |

6. Start the agent:
   - Simply double-click `ekiben-agent.exe` or run it from a terminal
   - The agent will read `agent-config.json` from the same directory and connect to your controller

7. That's it! The agent will connect to your controller and do its thing.

## Quick Info for Developers

- `ekiben-agent/` - The main agent for remote DB/API access and Jidotachi integration
- `cmd/dbcheck/` - A utility for peeking into the database (like counting users)
- `internal/` - All the core logic for WebSocket, queries, and validation

## Future Plans

- Cross-machine player data: If multiple machines run EKiBEN, player data can sync (if both sides agree)
- Custom song support: EKiBEN will have the capability to   TJA, OSZ and BMS files to the format used by the game. For legal reasons you should not use this on an actual cabinet, but it's there :P
