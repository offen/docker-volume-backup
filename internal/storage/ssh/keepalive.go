// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"sync"
	"time"
)

const keepAliveInterval = 30 * time.Second

type keepAliveClient interface {
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
	Close() error
}

// startKeepAlive keeps an established connection active until its cleanup is called.
// It does not wait for replies or attempt to reconnect a lost connection.
func startKeepAlive(client keepAliveClient, interval time.Duration) func() error {
	ticker := time.NewTicker(interval)
	stop := make(chan struct{})
	done := make(chan struct{})
	closeClient := sync.OnceValue(client.Close)

	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// A tick and shutdown can become ready at the same time.
				select {
				case <-stop:
					return
				default:
				}
				if _, _, err := client.SendRequest("keepalive", false, nil); err != nil {
					select {
					case <-stop:
						// Closing the transport also unblocks an in-flight request.
						return
					default:
					}
					// Unblock pending storage operations and let their error handling
					// report the failure. Cleanup returns the cached close error, if any.
					_ = closeClient()
					return
				}
			}
		}
	}()

	return sync.OnceValue(func() error {
		close(stop)
		ticker.Stop()
		// Close before waiting: SendRequest can be blocked writing to the transport.
		err := closeClient()
		<-done
		return err
	})
}
