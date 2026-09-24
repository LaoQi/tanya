package agent

func defaultConfig() *Config {
	return &Config{
		BaseURL:          "https://api.openai.com/v1",
		Model:            "deepseek-v4-flash",
		Temperature:      0.7,
		ApiProtocol:      "responses",
		SessionMode:      "auto",
		UserAgent:        DefaultUserAgent,
		DataDir:          "/tmp/tanya-test-data",
		ConfigPath:       "/tmp/tanya-test-config.yaml",
		AutoArchive:      true,
		ArchiveThreshold: DefaultArchiveThreshold,
		ArchiveKeep:      DefaultArchiveKeep,
	}
}
