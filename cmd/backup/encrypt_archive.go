// Copyright 2024 - Offen Authors <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"path"

	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
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
			return fmt.Errorf("encryptArchive: error removing gpg file: %w", err)
		}
		s.logger.Info(
			fmt.Sprintf("Removed GPG file `%s`.", gpgFile),
		)
		return nil
	})

	outFile, err := os.Create(gpgFile)
	if err != nil {
		return fmt.Errorf("encryptArchive: error opening out file: %w", err)
	}
	defer outFile.Close()

	_, name := path.Split(s.file)
	dst, err := openpgp.SymmetricallyEncrypt(outFile, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
		FileName: name,
	}, nil)
	if err != nil {
		return fmt.Errorf("encryptArchive: error encrypting backup file: %w", err)
	}
	defer dst.Close()

	src, err := os.Open(s.file)
	if err != nil {
		return fmt.Errorf("encryptArchive: error opening backup file `%s`: %w", s.file, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("encryptArchive: error writing ciphertext to file: %w", err)
	}

	s.file = gpgFile
	s.logger.Info(
		fmt.Sprintf("Encrypted backup using given passphrase, saving as `%s`.", s.file),
	)
	return nil
}
