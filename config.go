// config.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// DatabaseConfig struct for database connection details.
type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
}

// Config struct for overall application configuration.
type Config struct {
	Database    DatabaseConfig `json:"database"`
	ModulesPath string         `json:"modules_path"`
}

// LoadConfigFromFile reads configuration from a JSON file.
func LoadConfigFromFile(filePath string) (*Config, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("greška pri čitanju konfiguracionog fajla '%s': %w", filePath, err)
	}

	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		return nil, fmt.Errorf("greška pri parsiranju konfiguracionog fajla '%s': %w", filePath, err)
	}

	return &config, nil
}
