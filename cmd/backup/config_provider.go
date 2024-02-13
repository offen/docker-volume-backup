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

type configStrategy string

const (
	configStrategyEnv   configStrategy = "env"
	configStrategyConfd configStrategy = "confd"
)

// sourceConfiguration returns a list of config objects using the given
// strategy. It should be the single entrypoint for retrieving configuration
// for all consumers.
func sourceConfiguration(strategy configStrategy) ([]*Config, error) {
	switch strategy {
	case configStrategyEnv:
		c, err := loadConfigFromEnvVars()
		return []*Config{c}, err
	case configStrategyConfd:
		cs, err := loadConfigsFromEnvFiles("/etc/dockervolumebackup/conf.d")
		if err != nil {
			if os.IsNotExist(err) {
				return sourceConfiguration(configStrategyEnv)
			}
			return nil, fmt.Errorf("sourceConfiguration: error loading config files: %w", err)
		}
		return cs, nil
	default:
		return nil, fmt.Errorf("sourceConfiguration: received unknown config strategy: %v", strategy)
	}
}

// envProxy is a function that mimics os.LookupEnv but can read values from any other source
type envProxy func(string) (string, bool)

// loadConfig creates a config object using the given lookup function
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
		return nil, fmt.Errorf("loadConfig: failed to process configuration values: %w", err)
	}

	return c, nil
}

func loadConfigFromEnvVars() (*Config, error) {
	c, err := loadConfig(os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("loadEnvVars: error loading config from environment: %w", err)
	}
	c.source = "from environment"
	return c, nil
}

func loadConfigsFromEnvFiles(directory string) ([]*Config, error) {
	items, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("loadEnvFiles: failed to read files from env directory: %w", err)
	}

	configs := []*Config{}
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		p := filepath.Join(directory, item.Name())
		f, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("loadEnvFiles: error reading %s: %w", item.Name(), err)
		}
		envFile, err := godotenv.Unmarshal(os.ExpandEnv(string(f)))
		if err != nil {
			return nil, fmt.Errorf("loadEnvFiles: error reading config file %s: %w", p, err)
		}
		lookup := func(key string) (string, bool) {
			val, ok := envFile[key]
			if ok {
				return val, ok
			}
			return os.LookupEnv(key)
		}
		c, err := loadConfig(lookup)
		if err != nil {
			return nil, fmt.Errorf("loadEnvFiles: error loading config from file %s: %w", p, err)
		}
		c.source = item.Name()
		c.additionalEnvVars = envFile
		configs = append(configs, c)
	}

	return configs, nil
}
