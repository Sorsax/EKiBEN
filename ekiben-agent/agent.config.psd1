@{
  ServiceName = "EkibenAgent"
  Controller = "wss://your-controller.example/ws"
  Token = "YOUR_AGENT_TOKEN"
  AgentId = "agent-001"
  DbPath = "C:\\EKiBEN\\taiko.db3"
  AllowWrite = $false
  LogTraffic = $false
  UpdateRepo = "Sorsax/EKiBEN"
  UpdateAsset = "ekiben-agent.zip"
  Ping = "20s"
  Reconnect = "5s"
  Timeout = "10s"
}
