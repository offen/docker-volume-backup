// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"log"
	"os"

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
				log.Panicf("newScript: failed to read %s! Error: %s", location, err)
				return "", false
			}
			return string(contents), true
		case okValue && okFile: // both
			log.Panicf("newScript: both %s and %s are set!", key, key+"_FILE")
			return "", false
		default: // neither, ignore
			return "", false
		}
	}

	var c = &Config{}
	if err := envconfig.Process("", c); err != nil {
		return nil, fmt.Errorf("newScript: failed to process configuration values: %w", err)
	}

	return c, nil
}

func loadEnvVars() (*Config, error) {
	return loadConfig(os.LookupEnv)
}

func loadEnvFiles(folder string) ([]*Config, error) {
	items, err := os.ReadDir(folder)
	if err != nil {
		return nil, fmt.Errorf("Failed to read files from env folder: %w", err)
	}

	var cs = make([]*Config, 0)
	for _, item := range items {
		if item.IsDir() {
			log.Println("Skipping directory")
		} else {
			envFile, err := godotenv.Read(".env")
			if err != nil {
				log.Println("Error reading file: %w", err)
			}
			lookup := func(key string) (string, bool) {
				val, ok := envFile[key]
				return val, ok
			}
			c, err := loadConfig(lookup)
			if err != nil {
				log.Println("Error loading config from file: %w", err)
			}
			cs = append(cs, c)
		}
	}

	return cs, nil
}
