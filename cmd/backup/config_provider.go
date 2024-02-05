// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/offen/envconfig"
)

// envProxy is a function that mimics os.LookupEnv but can read values from any other source
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
				return "", false
			}
			return string(contents), true
		case okValue && okFile: // both
			return "", false
		default: // neither, ignore
			return "", false
		}
	}

	var c = &Config{}
	if err := envconfig.Process("", c); err != nil {
		return nil, fmt.Errorf("failed to process configuration values, error: %v", err)
	}

	return c, nil
}

func loadEnvVars() (*Config, error) {
	return loadConfig(os.LookupEnv)
}

func loadEnvFiles(directory string) ([]*Config, error) {
	items, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to read files from env directory, error: %v", err)
	}

	var cs = make([]*Config, 0)
	for _, item := range items {
		if !item.IsDir() {
			p := filepath.Join(directory, item.Name())
			envFile, err := godotenv.Read(p)
			if err != nil {
				return nil, fmt.Errorf("error reading config file %s, error: %v", p, err)
			}
			lookup := func(key string) (string, bool) {
				val, ok := envFile[key]
				return val, ok
			}
			c, err := loadConfig(lookup)
			if err != nil {
				return nil, fmt.Errorf("error loading config from file %s, error: %v", p, err)
			}
			cs = append(cs, c)
		}
	}

	return cs, nil
}
