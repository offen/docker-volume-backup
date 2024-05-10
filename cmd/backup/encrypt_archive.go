// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"path"

	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/jattento/docker-volume-backup/internal/errwrap"
)

// encryptArchive encrypts the backup file using PGP and the configured passphrase.
// In case no passphrase is given it returns early, leaving the backup file
// untouched.
func (s *script) encryptArchive() error {
	if s.c.GpgPassphrase == "" {
		return nil
	}

	gpgFile := fmt.Sprintf("%s.gpg", s.file)
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(gpgFile); err != nil {
			return errwrap.Wrap(err, "error removing gpg file")
		}
		s.logger.Info(
			fmt.Sprintf("Removed GPG file `%s`.", gpgFile),
		)
		return nil
	})

	outFile, err := os.Create(gpgFile)
	if err != nil {
		return errwrap.Wrap(err, "error opening out file")
	}
	defer outFile.Close()

	_, name := path.Split(s.file)
	dst, err := openpgp.SymmetricallyEncrypt(outFile, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
		FileName: name,
	}, nil)
	if err != nil {
		return errwrap.Wrap(err, "error encrypting backup file")
	}
	defer dst.Close()

	src, err := os.Open(s.file)
	if err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error opening backup file `%s`", s.file))
	}

	if _, err := io.Copy(dst, src); err != nil {
		return errwrap.Wrap(err, "error writing ciphertext to file")
	}

	s.file = gpgFile
	s.logger.Info(
		fmt.Sprintf("Encrypted backup using given passphrase, saving as `%s`.", s.file),
	)
	return nil
}
