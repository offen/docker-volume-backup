// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"
)

type command struct {
	logger *slog.Logger
}

func newCommand() *command {
	return &command{
		logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
}

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

func runScript(c *Config) (ret error) {
	defer func() {
		if err := recover(); err != nil {
			ret = fmt.Errorf("runScript: unexpected panic running script: %v", err)
		}
	}()

	s, err := newScript(c)
	if err != nil {
		return fmt.Errorf("runScript: error instantiating script: %w", err)
	}

	runErr := func() (ret error) {
		unlock, err := s.lock("/var/lock/dockervolumebackup.lock")
		if err != nil {
			return fmt.Errorf("runScript: error acquiring file lock: %w", err)
		}

		defer func() {
			err = unlock()
			if err != nil {
				ret = fmt.Errorf("runScript: error releasing file lock: %w", err)
			}
		}()

		scriptErr := func() error {
			if err := s.withLabeledCommands(lifecyclePhaseArchive, func() (ret error) {
				restartContainersAndServices, err := s.stopContainersAndServices()
				// The mechanism for restarting containers is not using hooks as it
				// should happen as soon as possible (i.e. before uploading backups or
				// similar).
				defer func() {
					ret = restartContainersAndServices()
				}()
				if err != nil {
					return err
				}
				return s.createArchive()
			})(); err != nil {
				return err
			}

			if err := s.withLabeledCommands(lifecyclePhaseProcess, s.encryptArchive)(); err != nil {
				return err
			}
			if err := s.withLabeledCommands(lifecyclePhaseCopy, s.copyArchive)(); err != nil {
				return err
			}
			if err := s.withLabeledCommands(lifecyclePhasePrune, s.pruneBackups)(); err != nil {
				return err
			}
			return nil
		}()
		if hookErr := s.runHooks(scriptErr); hookErr != nil {
			if scriptErr != nil {
				return fmt.Errorf(
					"runScript: error %w executing the script followed by %w calling the registered hooks",
					scriptErr,
					hookErr,
				)
			}
			return fmt.Errorf(
				"runScript: the script ran successfully, but an error occurred calling the registered hooks: %w",
				hookErr,
			)
		}
		if scriptErr != nil {
			return fmt.Errorf("runScript: error running script: %w", err)
		}
		return nil
	}()

	if runErr != nil {
		s.logger.Error(
			fmt.Sprintf("Script run failed: %v", runErr), "error", runErr,
		)
	}
	return runErr

}

func (c *command) runInForeground() error {
	cr := cron.New(
		cron.WithParser(
			cron.NewParser(
				cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
			),
		),
	)

	addJob := func(config *Config) error {
		if _, err := cr.AddFunc(config.BackupCronExpression, func() {
			if err := runScript(config); err != nil {
				c.logger.Error(
					fmt.Sprintf(
						"Unexpected error running schedule %v: %v",
						config.BackupCronExpression,
						err,
					),
					"error",
					err,
				)
			}
		}); err != nil {
			return fmt.Errorf("addJob: error adding schedule %s: %w", config.BackupCronExpression, err)
		}
		c.logger.Info(fmt.Sprintf("Successfully scheduled backup with expression %s", config.BackupCronExpression))
		return nil
	}

	cs, err := loadEnvFiles("/etc/dockervolumebackup/conf.d")
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("runInForeground: could not load config from environment files: %w", err)
		}

		c, err := loadEnvVars()
		if err != nil {
			return fmt.Errorf("runInForeground: could not load config from environment variables: %w", err)
		} else {
			err = addJob(c)
			if err != nil {
				return fmt.Errorf("runInForeground: error adding job from env: %w", err)
			}
		}
	} else {
		c.logger.Info("/etc/dockervolumebackup/conf.d was found, using configuration files from this directory.")
		for _, config := range cs {
			err = addJob(config)
			if err != nil {
				return fmt.Errorf("runInForeground: error adding jobs from conf files: %w", err)
			}
			c.logger.Info(
				fmt.Sprintf("Successfully scheduled backup with expression %s", config.BackupCronExpression),
			)
		}
	}

	var quit = make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	cr.Start()
	<-quit
	ctx := cr.Stop()
	<-ctx.Done()

	return nil
}

func (c *command) runAsCommand() error {
	config, err := loadEnvVars()
	if err != nil {
		return fmt.Errorf("could not load config from environment variables, error: %w", err)
	}

	err = runScript(config)
	if err != nil {
		return fmt.Errorf("unexpected error during backup, error: %w", err)
	}

	return nil
}

func main() {
	foreground := flag.Bool("foreground", false, "run the tool in the foreground")
	flag.Parse()

	c := newCommand()
	if *foreground {
		c.must(c.runInForeground())
	} else {
		c.must(c.runAsCommand())
	}
}
