# EKiBEN

### A multi-purpose toolkit for a certain very sugoi taiko server

> [!WARNING]
> Only use EKiBEN where it's allowed. If you run it on a vanilla network, which you definetly shouldn't, your machine might get banned. That's on you.

## What is EKiBEN?

EKiBEN is a toolkit for making a heavily modified instance of TLS actually useful. This specific TLS version by itself is extremely barebones. It writes and reads the database, that's it.

EKiBEN runs as a single Windows service agent on the same machine (usually a game cabinet) as the database. This agent gives you remote database access, API access, and Jidotachi integration. If more than two machines ever run EKiBEN, it'll support cross-database or machine player data (if both operators want it). 

For copyright reasons, I will not provide any front-end assets. If you have a legitimate copy of the game, you are able to source those on your own.

## Setup

**Note:** You are expected to source your game data legally from Bandai Namco. I do not provide any files related to the game or TLS.

1. Download the EKiBEN zip file.
2. Make a new folder (for example, `ta`) on the same drive as your TLS database (`taiko.db3`).
3. Extract everything from the zip into that folder.
4. Inside, you'll see:
   - `ekiben-agent.exe` (the main program)
   - `updater.exe` (for remotely updating the agent)
   - `agent.config.psd1` (service config file, only for running as a Windows service)
   - `install-service.ps1` (script to install the agent as a Windows service)
   - `uninstall-service.ps1` (script to remove the Windows service)
   - `start.bat` (for starting the agent manually, if you don't want to use the service)

5. Time to configure it:

   - If you want to run EKiBEN as a Windows service, open up `agent.config.psd1` in a text editor and fill in your details:

| Setting      | Example Value                              | What it does                                      |
| :-----------| :------------------------------------------| :------------------------------------------------ |
| ServiceName | EkibenAgent                                | Name for the Windows service                      |
| Controller  | wss://your-controller.example/ws           | WebSocket URL of your EKiBEN controller           |
| Token       | YOUR_AGENT_TOKEN                           | Authentication token for this agent               |
| AgentId     | agent-001                                  | Unique name for this agent                        |
| DbPath      | C:\\EKiBEN\\taiko.db3                   | Full path to your TLS database file               |
| AllowWrite  | $false                                     | $true to allow remote writes                      |
| LogTraffic  | $false                                     | $true to log all websocket traffic to console     |
| UpdateRepo  | Sorsax/EKiBEN                              | GitHub repo for releases (owner/name)             |
| UpdateAsset | ekiben-agent.zip                           | Release asset to download for updates             |
| Ping        | 20s                                        | How often to ping the controller                  |
| Reconnect   | 5s                                         | Wait time before reconnecting                     |
| Timeout     | 10s                                        | Request timeout                                   |

   - If you want to run it manually (not as a service), just use flags or environment variables instead of the config file. For example:
     ```bat
     ekiben-agent.exe --controller wss://your-controller.example/ws --token YOUR_TOKEN --agent-id agent-001 --db D:\Path\to\taiko.db3 --allow-write=false
     ```
     All the same settings as above are available as flags.

6. To start the agent:
   - For the service: First time Right-click `install-service.ps1` and pick "Run with PowerShell". After installation the service starts up with Windows.
   - For manual: Double-click `start.bat` or run your command in a terminal.

7. Updating:
   - The agent updates automatically whenever there is a new release (unless compiled from source). If you wish to update your configuration, edit the config file and run the install script again. This will update the service's configuration.

That's it! The agent will connect to your controller and do its thing.

## Quick Info for Developers

- `ekiben-agent/` - The main agent for remote DB/API access and Jidotachi integration
- `cmd/dbcheck/` - A utility for peeking into the database (like counting users)
- `internal/` - All the core logic for WebSocket, queries, and validation

## Future Plans

- Cross-machine player data: If multiple machines run EKiBEN, player data can sync (if both sides agree)
- Custom song support: EKiBEN will have the capability to convert TJA and BMS files to the format used by the game. For legal reasons you should not use this on an actual cabinet, but it's there :P
