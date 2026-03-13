package config

// GetMCPTransport returns the configured MCP transport
func (c *Config) GetMCPTransport() string {
	if c.MCP.Transport == "" {
		return "stdio"
	}
	return c.MCP.Transport
}
