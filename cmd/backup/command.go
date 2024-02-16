// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"
)

type command struct {
	logger    *slog.Logger
	schedules []cron.EntryID
	cr        *cron.Cron
	reload    chan struct{}
}

func newCommand() *command {
	return &command{
		logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
}

// runAsCommand executes a backup run for each configuration that is available
// and then returns
func (c *command) runAsCommand() error {
	configurations, err := sourceConfiguration(configStrategyEnv)
	if err != nil {
		return fmt.Errorf("runAsCommand: error loading env vars: %w", err)
	}

	for _, config := range configurations {
		if err := runScript(config); err != nil {
			return fmt.Errorf("runAsCommand: error running script: %w", err)
		}
	}

	return nil
}

type foregroundOpts struct {
	profileCronExpression string
}

// runInForeground starts the program as a long running process, scheduling
// a job for each configuration that is available.
func (c *command) runInForeground(opts foregroundOpts) error {
	c.cr = cron.New(
		cron.WithParser(
			cron.NewParser(
				cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
			),
		),
	)

	if err := c.schedule(configStrategyConfd); err != nil {
		return fmt.Errorf("runInForeground: error scheduling: %w", err)
	}

	if opts.profileCronExpression != "" {
		if _, err := c.cr.AddFunc(opts.profileCronExpression, c.profile); err != nil {
			return fmt.Errorf("runInForeground: error adding profiling job: %w", err)
		}
	}

	var quit = make(chan os.Signal, 1)
	c.reload = make(chan struct{}, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	c.cr.Start()

	for {
		select {
		case <-quit:
			ctx := c.cr.Stop()
			<-ctx.Done()
			return nil
		case <-c.reload:
			if err := c.schedule(configStrategyConfd); err != nil {
				return fmt.Errorf("runInForeground: error reloading configuration: %w", err)
			}
		}
	}
}

// schedule wipes all existing schedules and enqueues all schedules available
// using the given configuration strategy
func (c *command) schedule(strategy configStrategy) error {
	for _, id := range c.schedules {
		c.cr.Remove(id)
	}

	configurations, err := sourceConfiguration(strategy)
	if err != nil {
		return fmt.Errorf("schedule: error sourcing configuration: %w", err)
	}

	for _, cfg := range configurations {
		config := cfg
		id, err := c.cr.AddFunc(config.BackupCronExpression, func() {
			c.logger.Info(
				fmt.Sprintf(
					"Now running script on schedule %s",
					config.BackupCronExpression,
				),
			)

			if err := runScript(config); err != nil {
				c.logger.Error(
					fmt.Sprintf(
						"Unexpected error running schedule %s: %v",
						config.BackupCronExpression,
						err,
					),
					"error",
					err,
				)
			}
		})

		if err != nil {
			return fmt.Errorf("addJob: error adding schedule %s: %w", config.BackupCronExpression, err)
		}
		c.logger.Info(fmt.Sprintf("Successfully scheduled backup %s with expression %s", config.source, config.BackupCronExpression))
		if ok := checkCronSchedule(config.BackupCronExpression); !ok {
			c.logger.Warn(
				fmt.Sprintf("Scheduled cron expression %s will never run, is this intentional?", config.BackupCronExpression),
			)

			if err != nil {
				return fmt.Errorf("schedule: error scheduling: %w", err)
			}
			c.schedules = append(c.schedules, id)
		}
	}

	return nil
}

// must exits the program when passed an error. It should be the only
// place where the application exits forcefully.
func (c *command) must(err error) {
	if err != nil {
		c.logger.Error(
			fmt.Sprintf("Fatal error running command: %v", err),
			"error",
			err,
		)
		os.Exit(1)
	}
}
