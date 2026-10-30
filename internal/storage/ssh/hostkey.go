// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"errors"
	"net"
	"slices"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// hostKeyAlgorithms returns the host key algorithms that can be negotiated
// for a host key of the given type. RSA keys are signed with SHA-2 variants
// of the algorithm, so these are offered in addition to the key type itself.
func hostKeyAlgorithms(keyType string) []string {
	switch keyType {
	case ssh.KeyAlgoRSA:
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	case ssh.CertAlgoRSAv01:
		return []string{ssh.CertAlgoRSASHA512v01, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSAv01}
	default:
		return []string{keyType}
	}
}

// knownHostKeyAlgorithms returns the host key algorithms of all keys the
// known_hosts callback knows for the given host. It returns nil if no key is
// known, so the client's default preference is used.
func knownHostKeyAlgorithms(cb ssh.HostKeyCallback, hostName, port string) []string {
	// A key that matches no entry makes the callback report the known keys.
	err := cb(net.JoinHostPort(hostName, port), &net.TCPAddr{IP: net.IPv4zero}, unknownKey{})
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return nil
	}
	var algorithms []string
	for _, known := range keyErr.Want {
		for _, algorithm := range hostKeyAlgorithms(known.Key.Type()) {
			if !slices.Contains(algorithms, algorithm) {
				algorithms = append(algorithms, algorithm)
			}
		}
	}
	return algorithms
}

// unknownKey is a public key that matches no known_hosts entry.
type unknownKey struct{}

func (unknownKey) Type() string                            { return "unknown" }
func (unknownKey) Marshal() []byte                         { return []byte("unknown") }
func (unknownKey) Verify(_ []byte, _ *ssh.Signature) error { return errors.New("unknown key") }
