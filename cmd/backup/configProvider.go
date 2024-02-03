// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/offen/envconfig"
)

type envProxy func(string) (string, bool)

func loadConfig(lookup envProxy) (*Config, error) {
	envconfig.Lookup = func(key string) (string, bool) {
		value, okValue := lookup(key)
		location, okFile := lookup(key + "_FILE")

		switch {
		case okValue && !okFile: // only value
			return value, true
		case !okValue && okFile: // only file
			contents, err := os.ReadFile(location)
			if err != nil {
				slog.Error(fmt.Sprintf("failed to read %s", location), "error", err)
				return "", false
			}
			return string(contents), true
		case okValue && okFile: // both
			slog.Error(fmt.Sprintf("both %s and %s are set!", key, key+"_FILE"))
			return "", false
		default: // neither, ignore
			return "", false
		}
	}

	var c = &Config{}
	if err := envconfig.Process("", c); err != nil {
		slog.Error("failed to process configuration values", "error", err)
		return nil, err
	}

	return c, nil
}

func loadEnvVars() (*Config, error) {
	slog.Info("loading config from environment variables")
	return loadConfig(os.LookupEnv)
}

func loadEnvFiles(folder string) ([]*Config, error) {
	slog.Info(fmt.Sprintf("loading config from environment files from directory %s", folder))

	items, err := os.ReadDir(folder)
	if err != nil {
		slog.Error("failed to read files from env folder", "error", err)
		return nil, err
	}

	var cs = make([]*Config, 0)
	for _, item := range items {
		if item.IsDir() {
			slog.Info("skipping subdirectory")
		} else {
			p := filepath.Join(folder, item.Name())
			envFile, err := godotenv.Read(p)
			if err != nil {
				slog.Error(fmt.Sprintf("skipping subdirectory %s", p), "error", err)
				return nil, err
			}
			lookup := func(key string) (string, bool) {
				val, ok := envFile[key]
				return val, ok
			}
			c, err := loadConfig(lookup)
			if err != nil {
				slog.Error(fmt.Sprintf("error loading config from file %s", p), "error", err)
				return nil, err
			}
			cs = append(cs, c)
		}
	}

	return cs, nil
}
