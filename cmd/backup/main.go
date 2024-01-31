// Copyright 2021-2022 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"
)

func runBackup(c *Config) {
	s, err := newScript(c)
	if err != nil {
		panic(err)
	}

	unlock, err := s.lock("/var/lock/dockervolumebackup.lock")
	defer func() {
		s.must(unlock())
	}()
	s.must(err)

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
				os.Exit(1)
			}
			panic(pArg)
		}

		if err := s.runHooks(nil); err != nil {
			s.logger.Error(
				fmt.Sprintf(
					"Backup procedure ran successfully, but an error ocurred calling the registered hooks: %v",
					err,
				),
			)
			os.Exit(1)
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
}

func main() {
	var serve = flag.Bool("foreground", false, "run the backup in the foreground")
	var envFolder = flag.String("env-folder", "/etc/dockervolumebackup/conf.d", "location of environment files")
	flag.Parse()

	if *serve {
		log.Println("Running in the foreground instead of cron")

		cr := cron.New()

		c, err := loadEnvVars()
		if err != nil {
			log.Println("Could not load config from environment variables")
		} else {
			log.Println("Added cron job with schedule: ", c.BackupCronExpression)
			cr.AddFunc(c.BackupCronExpression, func() { runBackup(c) })
		}

		cs, err := loadEnvFiles(*envFolder)
		if err != nil {
			log.Println("Could not load config from environment files")
		} else {
			for _, c := range cs {
				log.Println("Added cron job with schedule: ", c.BackupCronExpression)
				cr.AddFunc(c.BackupCronExpression, func() { runBackup(c) })
			}
		}

		log.Println("Subscribed to interupt signals")
		var quit = make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

		log.Println("Starting cron scheduler")
		cr.Start()

		log.Println("Application goes to sleep")
		<-quit

		log.Println("Interrupt arrived, stopping schedules")
		ctx := cr.Stop()
		<-ctx.Done()
	} else {
		log.Println("Executing one time backup")

		c, err := loadEnvVars()
		if err != nil {
			log.Println("Could not load config from environment variables")
		}

		runBackup(c)
	}
}
