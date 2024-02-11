// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
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

func runScript(c *Config) (err error) {
	defer func() {
		if derr := recover(); derr != nil {
			err = fmt.Errorf("runScript: unexpected panic running script: %v", err)
		}
	}()

	s, err := newScript(c)
	if err != nil {
		err = fmt.Errorf("runScript: error instantiating script: %w", err)
		return
	}

	runErr := func() (err error) {
		unlock, err := s.lock("/var/lock/dockervolumebackup.lock")
		if err != nil {
			err = fmt.Errorf("runScript: error acquiring file lock: %w", err)
			return
		}

		defer func() {
			derr := unlock()
			if err == nil && derr != nil {
				err = fmt.Errorf("runScript: error releasing file lock: %w", derr)
			}
		}()

		scriptErr := func() error {
			if err := s.withLabeledCommands(lifecyclePhaseArchive, func() (err error) {
				restartContainersAndServices, err := s.stopContainersAndServices()
				// The mechanism for restarting containers is not using hooks as it
				// should happen as soon as possible (i.e. before uploading backups or
				// similar).
				defer func() {
					derr := restartContainersAndServices()
					if err == nil {
						err = derr
					}
				}()
				if err != nil {
					return
				}
				err = s.createArchive()
				return
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
			return fmt.Errorf("runScript: error running script: %w", scriptErr)
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

func (c *command) runInForeground(profileCronExpression string) error {
	cr := cron.New(
		cron.WithParser(
			cron.NewParser(
				cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
			),
		),
	)

	addJob := func(config *Config, name string) error {
		if _, err := cr.AddFunc(config.BackupCronExpression, func() {
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
		}); err != nil {
			return fmt.Errorf("addJob: error adding schedule %s: %w", config.BackupCronExpression, err)
		}

		c.logger.Info(fmt.Sprintf("Successfully scheduled backup %s with expression %s", name, config.BackupCronExpression))
		if ok := checkCronSchedule(config.BackupCronExpression); !ok {
			c.logger.Warn(
				fmt.Sprintf("Scheduled cron expression %s will never run, is this intentional?", config.BackupCronExpression),
			)
		}

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
			err = addJob(c, "from environment")
			if err != nil {
				return fmt.Errorf("runInForeground: error adding job from env: %w", err)
			}
		}
	} else {
		c.logger.Info("/etc/dockervolumebackup/conf.d was found, using configuration files from this directory.")
		for _, config := range cs {
			err = addJob(config.config, config.name)
			if err != nil {
				return fmt.Errorf("runInForeground: error adding jobs from conf files: %w", err)
			}
		}
	}

	if profileCronExpression != "" {
		if _, err := cr.AddFunc(profileCronExpression, func() {
			memStats := runtime.MemStats{}
			runtime.ReadMemStats(&memStats)
			c.logger.Info(
				"Collecting runtime information",
				"num_goroutines",
				runtime.NumGoroutine(),
				"memory_heap_alloc",
				formatBytes(memStats.HeapAlloc, false),
				"memory_heap_inuse",
				formatBytes(memStats.HeapInuse, false),
				"memory_heap_sys",
				formatBytes(memStats.HeapSys, false),
				"memory_heap_objects",
				memStats.HeapObjects,
			)
		}); err != nil {
			return fmt.Errorf("runInForeground: error adding profiling job: %w", err)
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
		return fmt.Errorf("runAsCommand: error loading env vars: %w", err)
	}
	err = runScript(config)
	if err != nil {
		return fmt.Errorf("runAsCommand: error running script: %w", err)
	}

	return nil
}

func main() {
	foreground := flag.Bool("foreground", false, "run the tool in the foreground")
	profile := flag.String("profile", "", "collect runtime metrics and log them periodically on the given cron expression")
	flag.Parse()

	c := newCommand()
	if *foreground {
		c.must(c.runInForeground(*profile))
	} else {
		c.must(c.runAsCommand())
	}
}
