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

	"filippo.io/age"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/offen/docker-volume-backup/internal/errwrap"
)

func countTrue(b ...bool) int {
	c := int(0)
	for _, v := range b {
		if v {
			c++
		}
	}
	return c
}

// encryptArchive encrypts the backup file using PGP and the configured passphrase or publickey(s).
// In case no passphrase or publickey is given it returns early, leaving the backup file
// untouched.
func (s *script) encryptArchive() error {
	useGPGSymmetric := s.c.GpgPassphrase != ""
	useGPGAsymmetric := s.c.GpgPublicKeyRing != ""
	useAgeSymmetric := s.c.AgePassphrase != ""
	useAgeAsymmetric := len(s.c.AgePublicKeys) > 0
	switch nconfigured := countTrue(
		useGPGSymmetric,
		useGPGAsymmetric,
		useAgeSymmetric,
		useAgeAsymmetric,
	); nconfigured {
	case 0:
		return nil
	case 1:
		// ok!
	default:
		return fmt.Errorf(
			"error in selecting archive encryption method: expected 0 or 1 to be configured, %d methods are configured",
			nconfigured,
		)
	}

	if useGPGSymmetric {
		return s.encryptWithGPGSymmetric()
	} else if useGPGAsymmetric {
		return s.encryptWithGPGAsymmetric()
	} else if useAgeSymmetric || useAgeAsymmetric {
		ar, err := s.getConfiguredAgeRecipients()
		if err != nil {
			return errwrap.Wrap(err, "failed to get configured age recipients")
		}
		return s.encryptWithAge(ar)
	}
	return nil
}

func (s *script) getConfiguredAgeRecipients() ([]age.Recipient, error) {
	if s.c.AgePassphrase == "" && len(s.c.AgePublicKeys) == 0 {
		return nil, fmt.Errorf("no age recipients configured")
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

func (s *script) encryptWithGPGSymmetric() error {
	return s.doEncrypt("gpg", func(ciphertextWriter io.Writer) (io.WriteCloser, error) {
		_, name := path.Split(s.file)
		return openpgp.SymmetricallyEncrypt(ciphertextWriter, []byte(s.c.GpgPassphrase), &openpgp.FileHints{
			FileName: name,
		}, nil)
	})
}

type closeAllWriter struct {
	io.Writer
	closers []io.Closer
}

func (c *closeAllWriter) Close() (err error) {
	for _, cl := range c.closers {
		err = errors.Join(err, cl.Close())
	}
	return
}

var _ io.WriteCloser = (*closeAllWriter)(nil)

func (s *script) encryptWithGPGAsymmetric() error {
	return s.doEncrypt("gpg", func(ciphertextWriter io.Writer) (_ io.WriteCloser, outerr error) {
		entityList, err := openpgp.ReadArmoredKeyRing(bytes.NewReader([]byte(s.c.GpgPublicKeyRing)))
		if err != nil {
			return nil, errwrap.Wrap(err, "error parsing armored keyring")
		}

		armoredWriter, err := armor.Encode(ciphertextWriter, "PGP MESSAGE", nil)
		if err != nil {
			return nil, errwrap.Wrap(err, "error preparing encryption")
		}
		defer func() {
			if outerr != nil {
				_ = armoredWriter.Close()
			}
		}()

		_, name := path.Split(s.file)
		encWriter, err := openpgp.Encrypt(armoredWriter, entityList, nil, nil, &openpgp.FileHints{
			FileName: name,
		}, nil)
		if err != nil {
			return nil, err
		}
		return &closeAllWriter{
			Writer:  encWriter,
			closers: []io.Closer{encWriter, armoredWriter},
		}, nil
	})
}

func (s *script) doEncrypt(
	extension string,
	encryptor func(ciphertextWriter io.Writer) (io.WriteCloser, error),
) (outerr error) {
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
	defer func() {
		if err := outFile.Close(); err != nil {
			outerr = errors.Join(outerr, errwrap.Wrap(err, "error closing out file"))
		}
	}()

	dst, err := encryptor(outFile)
	if err != nil {
		return errwrap.Wrap(err, "error encrypting backup file")
	}
	defer func() {
		if err := dst.Close(); err != nil {
			outerr = errors.Join(outerr, errwrap.Wrap(err, "error closing encrypted backup file"))
		}
	}()

	src, err := os.Open(s.file)
	if err != nil {
		return errwrap.Wrap(err, fmt.Sprintf("error opening backup file %q", s.file))
	}
	defer func() {
		if err := src.Close(); err != nil {
			outerr = errors.Join(outerr, errwrap.Wrap(err, "error closing backup file"))
		}
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return errwrap.Wrap(err, "error writing ciphertext to file")
	}

	s.file = encFile
	s.logger.Info(
		fmt.Sprintf("Encrypted backup using %q, saving as %q", extension, s.file),
	)

	return
}
