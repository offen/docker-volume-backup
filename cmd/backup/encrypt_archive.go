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

func (s *script) encryptAsymmetrically(outFile *os.File) (io.WriteCloser, func() error, error) {

	entityList, err := openpgp.ReadArmoredKeyRing(bytes.NewReader([]byte(s.c.GpgPublicKeyRing)))
	if err != nil {
		return nil, nil, errwrap.Wrap(err, fmt.Sprintf("error parsing key: %v", err))
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
		if err := dst.Close(); err != nil {
			return err
		}
		return armoredWriter.Close()
	}, err
}

func (s *script) encryptSymmetrically(outFile *os.File) (io.WriteCloser, func() error, error) {

	_, name := path.Split(s.file)
	dst, err := openpgp.SymmetricallyEncrypt(outFile, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
		FileName: name,
	}, nil)
	if err != nil {
		return nil, nil, err
	}

	return dst, dst.Close, nil
}

// encryptArchive encrypts the backup file using PGP and the configured passphrase or publickey(s).
// In case no passphrase or publickey is given it returns early, leaving the backup file
// untouched.
func (s *script) encryptArchive() error {

	var encrypt func(outFile *os.File) (io.WriteCloser, func() error, error)
	var cleanUpErr error

	switch {
	case s.c.GpgPassphrase != "" && s.c.GpgPublicKeyRing != "":
		return errwrap.Wrap(nil, "error in selecting asymmetric and symmetric encryption methods: conflicting env vars are set")
	case s.c.GpgPassphrase != "":
		encrypt = s.encryptSymmetrically
	case s.c.GpgPublicKeyRing != "":
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
	defer func() {
		if err := outFile.Close(); err != nil {
			cleanUpErr = errors.Join(cleanUpErr, errwrap.Wrap(err, "error closing out file"))
		}
	}()

	dst, dstCloseCallback, err := encrypt(outFile)
	if err != nil {
		return errwrap.Wrap(err, "error encrypting backup file")
	}
	defer func() {
		if err := dstCloseCallback(); err != nil {
			cleanUpErr = errors.Join(cleanUpErr, errwrap.Wrap(err, "error closing encrypted backup file"))
		}
	}()

	src, err := os.Open(s.file)
	if err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error opening backup file `%s`", s.file))
	}
	defer func() {
		if err := src.Close(); err != nil {
			cleanUpErr = errors.Join(cleanUpErr, errwrap.Wrap(err, "error closing backup file"))
		}
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return errwrap.Wrap(err, "error writing ciphertext to file")
	}

	s.file = gpgFile
	s.logger.Info(
		fmt.Sprintf("Encrypted backup using given passphrase, saving as `%s`.", s.file),
	)
	return cleanUpErr
}
