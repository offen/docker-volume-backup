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

func app(serve bool) error {
	if serve {
		cr := cron.New(
			cron.WithParser(
				cron.NewParser(
					cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
				),
			),
		)

		addJob := func(c *Config) error {
			_, err := cr.AddFunc(c.BackupCronExpression, func() {
				err := runBackup(c)
				if err != nil {
					slog.Error("unexpected error during backup", "error", err)
				}
			})
			return err
		}

		cs, err := loadEnvFiles("/etc/dockervolumebackup/conf.d")
		if err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("could not load config from environment files, error: %v", err)
			}

			c, err := loadEnvVars()
			if err != nil {
				return fmt.Errorf("could not load config from environment variables")
			} else {
				err = addJob(c)
				if err != nil {
					return fmt.Errorf("could not add cron job, error: %v", err)
				}
			}
		} else {
			for _, c := range cs {
				err = addJob(c)
				if err != nil {
					return fmt.Errorf("could not add cron job, error: %v", err)
				}
			}
		}

		var quit = make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
		cr.Start()
		<-quit
		ctx := cr.Stop()
		<-ctx.Done()
	} else {
		c, err := loadEnvVars()
		if err != nil {
			return fmt.Errorf("could not load config from environment variables, error: %v", err)
		}

		err = runBackup(c)
		if err != nil {
			return fmt.Errorf("unexpected error during backup, error: %v", err)
		}
	}

	return nil
}

func main() {
	serve := flag.Bool("foreground", false, "run the tool in the foreground")
	flag.Parse()

	err := app(*serve)
	if err != nil {
		slog.Error("ran into an issue during execution", "error", err)
		os.Exit(1)
	}
}
