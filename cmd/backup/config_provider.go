// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jattento/docker-volume-backup/internal/errwrap"
	"github.com/joho/godotenv"
	"github.com/offen/envconfig"
	shell "mvdan.cc/sh/v3/shell"
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
			return nil, errwrap.Wrap(err, "error loading config files")
		}
		return cs, nil
	default:
		return nil, errwrap.Wrap(nil, fmt.Sprintf("received unknown config strategy: %v", strategy))
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
		return nil, errwrap.Wrap(err, "failed to process configuration values")
	}

	return c, nil
}

func loadConfigFromEnvVars() (*Config, error) {
	c, err := loadConfig(os.LookupEnv)
	if err != nil {
		return nil, errwrap.Wrap(err, "error loading config from environment")
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
		return nil, errwrap.Wrap(err, "failed to read files from env directory")
	}

	configs := []*Config{}
	for _, item := range items {
		if item.IsDir() {
			continue
		}
		p := filepath.Join(directory, item.Name())
		envFile, err := source(p)
		if err != nil {
			return nil, errwrap.Wrap(err, fmt.Sprintf("error reading config file %s", p))
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
			return nil, errwrap.Wrap(err, fmt.Sprintf("error loading config from file %s", p))
		}
		c.source = item.Name()
		c.additionalEnvVars = envFile
		configs = append(configs, c)
	}

	return configs, nil
}

// source tries to mimic the pre v2.37.0 behavior of calling
// `set +a; source $path; set -a` and returns the env vars as a map
func source(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errwrap.Wrap(err, fmt.Sprintf("error opening %s", path))
	}

	result := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		withExpansion, err := shell.Expand(line, nil)
		if err != nil {
			return nil, errwrap.Wrap(err, "error expanding env")
		}
		m, err := godotenv.Unmarshal(withExpansion)
		if err != nil {
			return nil, errwrap.Wrap(err, fmt.Sprintf("error sourcing %s", path))
		}
		for key, value := range m {
			currentValue, currentOk := os.LookupEnv(key)
			defer func() {
				if currentOk {
					os.Setenv(key, currentValue)
					return
				}
				os.Unsetenv(key)
			}()
			result[key] = value
			os.Setenv(key, value)
		}
	}
	return result, nil
}
