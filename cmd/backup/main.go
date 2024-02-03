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

func runBackup(c *Config) (ret error) {
	s, err := newScript(c)
	if err != nil {
		return err
	}

	unlock, err := s.lock("/var/lock/dockervolumebackup.lock")
	if err != nil {
		return err
	}

	defer func() {
		err = unlock()
		if err != nil {
			ret = err
		}
	}()

	defer func() {
		if pArg := recover(); pArg != nil {
			if err, ok := pArg.(error); ok {
				s.logger.Error(
					fmt.Sprintf("Executing the script encountered a panic: %v", err),
				)
				if hookErr := s.runHooks(err); hookErr != nil {
					s.logger.Error(
						fmt.Sprintf("An error occurred calling the registered hooks: %s", hookErr),
					)
				}
				ret = err
			} else {
				s.logger.Error(
					fmt.Sprintf("Executing the script encountered an unrecoverable panic: %v", err),
				)

				panic(pArg)
			}
		}

		if err := s.runHooks(nil); err != nil {
			s.logger.Error(
				fmt.Sprintf(
					"Backup procedure ran successfully, but an error ocurred calling the registered hooks: %v",
					err,
				),
			)
			ret = err
		}
		s.logger.Info("Finished running backup tasks.")
	}()

	s.must(s.withLabeledCommands(lifecyclePhaseArchive, func() error {
		restartContainersAndServices, err := s.stopContainersAndServices()
		// The mechanism for restarting containers is not using hooks as it
		// should happen as soon as possible (i.e. before uploading backups or
		// similar).
		defer func() {
			s.must(restartContainersAndServices())
		}()
		if err != nil {
			return err
		}
		return s.createArchive()
	})())

	s.must(s.withLabeledCommands(lifecyclePhaseProcess, s.encryptArchive)())
	s.must(s.withLabeledCommands(lifecyclePhaseCopy, s.copyArchive)())
	s.must(s.withLabeledCommands(lifecyclePhasePrune, s.pruneBackups)())

	return nil
}

func main() {
	serve := flag.Bool("foreground", false, "run the backup in the foreground")
	envFolder := flag.String("env-folder", "/etc/dockervolumebackup/conf.d", "location of environment files")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if *serve {
		logger.Info("running in the foreground instead of cron")

		cr := cron.New()

		addJob := func(c *Config) {
			logger.Info("added cron job", "schedule", c.BackupCronExpression)
			_, err := cr.AddFunc(c.BackupCronExpression, func() {
				err := runBackup(c)
				if err != nil {
					logger.Error("unexpected error during backup", "error", err)
				}
			})
			if err != nil {
				logger.Error("failed to create cron job", "schedule", c.BackupCronExpression)
			}
		}

		cs, err := loadEnvFiles(*envFolder)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Error("could not load config from environment files")
				os.Exit(1)
			}

			c, err := loadEnvVars()
			if err != nil {
				logger.Error("could not load config from environment variables")
				os.Exit(1)
			} else {
				addJob(c)
			}
		} else {
			for _, c := range cs {
				addJob(c)
			}
		}

		logger.Info("subscribed to interupt signals")
		var quit = make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

		logger.Info("starting cron scheduler")
		cr.Start()

		logger.Info("application goes to sleep")
		<-quit

		logger.Info("interrupt arrived, stopping schedules")
		ctx := cr.Stop()
		<-ctx.Done()
	} else {
		logger.Info("executing one time backup")

		c, err := loadEnvVars()
		if err != nil {
			logger.Info("could not load config from environment variables", "error", err)
			os.Exit(1)
		}

		err = runBackup(c)
		if err != nil {
			logger.Info("unexpected error during backup", "error", err)
			os.Exit(1)
		}
	}

	os.Exit(0)
}
