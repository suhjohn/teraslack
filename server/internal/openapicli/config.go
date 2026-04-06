package openapicli

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func loadFileConfig() (fileConfig, error) {
	path, err := configFilePath()
	if err != nil {
		return fileConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileConfig{}, nil
		}
		return fileConfig{}, err
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, err
	}
	return cfg, nil
}

func saveFileConfig(cfg fileConfig) error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func clearSessionFromFileConfig() error {
	cfg, err := loadFileConfig()
	if err != nil {
		return err
	}
	cfg.SessionToken = ""
	cfg.WorkspaceID = ""
	cfg.UserID = ""
	return saveFileConfig(cfg)
}

func writeSessionToFileConfig(baseURL string, sessionToken string, userID string) error {
	cfg, err := loadFileConfig()
	if err != nil {
		return err
	}
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	cfg.SessionToken = sessionToken
	cfg.UserID = userID
	return saveFileConfig(cfg)
}
