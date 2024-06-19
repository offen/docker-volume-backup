// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"path"

	"filippo.io/age"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/offen/docker-volume-backup/internal/errwrap"
)

// encryptArchive encrypts the backup file using PGP and the configured passphrase.
// In case no passphrase is given it returns early, leaving the backup file
// untouched.
func (s *script) encryptArchive() error {
	if s.c.GpgPassphrase != "" {
		if s.c.AgePassphrase != "" || len(s.c.AgePublicKeys) > 0 {
			return fmt.Errorf("GPG and age cannot be configured simultaneously")
		}

		if err := s.encryptWithGPG(); err != nil {
			return errwrap.Wrap(err, "gpg encryption failed")
		}
		return nil
	}
	if ar, err := s.getConfiguredAgeRecipients(); err != nil {
		return errwrap.Wrap(err, "failed to get configured age recipients")
	} else if len(ar) > 0 {
		if err := s.encryptWithAge(ar); err != nil {
			return errwrap.Wrap(err, "age encryption failed")
		}
		return nil
	}

	return nil
}

func (s *script) getConfiguredAgeRecipients() ([]age.Recipient, error) {
	if s.c.AgePassphrase == "" && len(s.c.AgePublicKeys) == 0 {
		// a little redundant to check config here _AND_ below, but nice to
		// avoid the allocation if we can
		return nil, nil
	}
	recipients := []age.Recipient{}
	if len(s.c.AgePublicKeys) > 0 {
		for _, pk := range s.c.AgePublicKeys {
			pkr, err := age.ParseX25519Recipient(pk)
			if err != nil {
				return nil, errwrap.Wrap(err, "failed to parse age public key")
			}
			recipients = append(recipients, pkr)
		}
	}
	if s.c.AgePassphrase != "" {
		if len(recipients) != 0 {
			return nil, fmt.Errorf("age encryption must only be enabled via passphrase or public key, not both")
		}

		r, err := age.NewScryptRecipient(s.c.AgePassphrase)
		if err != nil {
			return nil, errwrap.Wrap(err, "failed to create scrypt identity from age passphrase")
		}
		recipients = append(recipients, r)
	}
	return recipients, nil
}

func (s *script) encryptWithAge(rec []age.Recipient) error {
	return s.doEncrypt("age", func(ciphertextWriter io.Writer) (io.WriteCloser, error) {
		return age.Encrypt(ciphertextWriter, rec...)
	})
}

func (s *script) encryptWithGPG() error {
	return s.doEncrypt("gpg", func(ciphertextWriter io.Writer) (io.WriteCloser, error) {
		_, name := path.Split(s.file)
		return openpgp.SymmetricallyEncrypt(ciphertextWriter, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
			FileName: name,
		}, nil)
	})
}

func (s *script) doEncrypt(
	extension string,
	encryptor func(ciphertextWriter io.Writer) (io.WriteCloser, error),
) error {
	encFile := fmt.Sprintf("%s.%s", s.file, extension)
	s.registerHook(hookLevelPlumbing, func(error) error {
		if err := remove(encFile); err != nil {
			return errwrap.Wrap(err, "error removing encrypted file")
		}
		s.logger.Info(
			fmt.Sprintf("Removed encrypted file `%s`.", encFile),
		)
		return nil
	})

	outFile, err := os.Create(encFile)
	if err != nil {
		return errwrap.Wrap(err, "error opening out file")
	}
	defer outFile.Close()

	dst, err := encryptor(outFile)
	if err != nil {
		return errwrap.Wrap(err, "error encrypting backup file")
	}
	defer dst.Close()

	src, err := os.Open(s.file)
	if err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error opening backup file `%s`", s.file))
	}
	defer src.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return errwrap.Wrap(err, "error writing ciphertext to file")
	}

	s.file = encFile
	s.logger.Info(
		fmt.Sprintf("Encrypted backup using given configuration, saving as `%s`.", s.file),
		"encryptor", extension,
	)
	return nil
}
