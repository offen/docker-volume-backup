// Copyright 2024 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/offen/docker-volume-backup/internal/errwrap"
)

func readArmoredKeys(data []byte) (openpgp.EntityList, error) {
	block, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return block, nil
}

func (s *script) encryptAsymmetrically(outFile *os.File) (io.WriteCloser, func() error, error) {
	if s.c.GpgPublicKeys == "" {
		return nil, nil, nil
	}

	entityList, err := readArmoredKeys([]byte(s.c.GpgPublicKeys))
	if err != nil {
		return nil, nil, errwrap.Wrap(err, fmt.Sprintf("Error parsing key: %v", err))
	}

	armoredWriter, err := armor.Encode(outFile, "PGP MESSAGE", nil)
	if err != nil {
		return nil, nil, errwrap.Wrap(err, "error preparing encryption")
	}

	_, name := path.Split(s.file)
	dst, err := openpgp.Encrypt(armoredWriter, entityList, nil, nil, &openpgp.FileHints{
		FileName: name,
	}, nil)
	if err != nil {
		return nil, nil, err
	}

	return dst, func() error {
		dst.Close()
		armoredWriter.Close()
		return nil
	}, err
}

func (s *script) encryptSymmetrically(outFile *os.File) (io.WriteCloser, func() error, error) {
	if s.c.GpgPassphrase == "" {
		return nil, nil, nil
	}

	_, name := path.Split(s.file)
	dst, err := openpgp.SymmetricallyEncrypt(outFile, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
		FileName: name,
	}, nil)
	if err != nil {
		return nil, nil, err
	}

	return dst, dst.Close, nil
}

// encryptArchive encrypts the backup file using PGP and the configured passphrase.
// In case no passphrase is given it returns early, leaving the backup file
// untouched.
func (s *script) encryptArchive() error {

	var encrypt func(outFile *os.File) (io.WriteCloser, func() error, error)

	switch {
	case s.c.GpgPassphrase != "" && s.c.GpgPublicKeys != "":
		return errors.New("error in selecting asymmetric and symmetric encryption methods: conflicting env vars are set")
	case s.c.GpgPassphrase != "":
		encrypt = s.encryptSymmetrically
	case s.c.GpgPublicKeys != "":
		encrypt = s.encryptAsymmetrically
	default:
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

	dst, closeCallback, err := encrypt(outFile)
	if err != nil {
		return errwrap.Wrap(err, "error encrypting backup file")
	}
	defer closeCallback()

	src, err := os.Open(s.file)
	if err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error opening backup file `%s`", s.file))
	}
	defer src.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return errwrap.Wrap(err, "error writing ciphertext to file")
	}

	s.file = gpgFile
	s.logger.Info(
		fmt.Sprintf("Encrypted backup using given passphrase, saving as `%s`.", s.file),
	)
	return nil
}
