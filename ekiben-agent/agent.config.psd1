@{
  ServiceName = "EkibenAgent"
  Controller = "wss://your-controller.example/ws"
  Token = "YOUR_AGENT_TOKEN"
  AgentId = "agent-001"
  DbPath = "D:\\Webbivelhoilut\\EKiBEN\\taiko.db3"
  AllowWrite = $false
  Ping = "20s"
  Reconnect = "5s"
  Timeout = "10s"
}
