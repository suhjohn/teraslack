package openapicli

import (
	"encoding/json"
	"os"
	"path/filepath"
)

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
	path, err := configFilePath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	cfg, err := loadFileConfig()
	if err != nil {
		return err
	}
	cfg.SessionToken = ""
	cfg.WorkspaceID = ""
	cfg.UserID = ""
	return saveFileConfig(cfg)
}

func writeSessionToFileConfig(baseURL string, sessionToken string, auth *authMeResponse) error {
	cfg, err := loadFileConfig()
	if err != nil {
		return err
	}
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	cfg.SessionToken = sessionToken
	if auth != nil {
		cfg.WorkspaceID = auth.WorkspaceID
		cfg.UserID = auth.UserID
	}
	return saveFileConfig(cfg)
}
