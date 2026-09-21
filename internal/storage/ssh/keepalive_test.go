// Copyright 2026 - offen.software <hioffen@posteo.de>
// SPDX-License-Identifier: MPL-2.0

package ssh

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

type mockKeepAliveClient struct {
	sendRequest func(string, bool, []byte) (bool, []byte, error)
	close       func() error
}

func (m *mockKeepAliveClient) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return m.sendRequest(name, wantReply, payload)
}

func (m *mockKeepAliveClient) Close() error {
	return m.close()
}

func TestKeepAliveSendsPeriodicRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests, closes int
		client := &mockKeepAliveClient{
			sendRequest: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
				requests++
				if name != "keepalive" || wantReply || payload != nil {
					t.Errorf("SendRequest(%q, %v, %v), want keepalive, false, nil", name, wantReply, payload)
				}
				return false, nil, nil
			},
			close: func() error {
				closes++
				return nil
			},
		}
		cleanup := startKeepAlive(client, keepAliveInterval)
		t.Cleanup(func() { _ = cleanup() })

		time.Sleep(keepAliveInterval - time.Nanosecond)
		synctest.Wait()
		if requests != 0 {
			t.Fatalf("Sent %d requests before the first interval", requests)
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if requests != 1 {
			t.Fatalf("Sent %d requests after the first interval, want 1", requests)
		}
		time.Sleep(2 * keepAliveInterval)
		synctest.Wait()
		if requests != 3 {
			t.Fatalf("Sent %d requests after three intervals, want 3", requests)
		}

		if err := cleanup(); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
		time.Sleep(2 * keepAliveInterval)
		synctest.Wait()
		if requests != 3 || closes != 1 {
			t.Errorf("After cleanup: requests=%d, closes=%d; want 3, 1", requests, closes)
		}
	})
}

func TestKeepAliveSendFailureClosesClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sendErr := errors.New("connection lost")
		closeErr := errors.New("close failed")
		var requests, closes int
		client := &mockKeepAliveClient{
			sendRequest: func(string, bool, []byte) (bool, []byte, error) {
				requests++
				return false, nil, sendErr
			},
			close: func() error {
				closes++
				return closeErr
			},
		}
		cleanup := startKeepAlive(client, keepAliveInterval)
		t.Cleanup(func() { _ = cleanup() })

		time.Sleep(3 * keepAliveInterval)
		synctest.Wait()
		if requests != 1 || closes != 1 {
			t.Fatalf("After send failure: requests=%d, closes=%d; want 1 each", requests, closes)
		}
		if err := cleanup(); !errors.Is(err, closeErr) {
			t.Fatalf("Cleanup after send failure: got %v, want %v", err, closeErr)
		}
		if err := cleanup(); !errors.Is(err, closeErr) {
			t.Errorf("Repeated cleanup after send failure: got %v, want %v", err, closeErr)
		}
		if closes != 1 {
			t.Errorf("Cleanup closed the failed client again: closes=%d", closes)
		}
	})
}

func TestKeepAliveCleanupUnblocksAndWaitsForSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		closed := make(chan struct{})
		var requests, closes int
		var sendFinished bool
		client := &mockKeepAliveClient{
			sendRequest: func(string, bool, []byte) (bool, []byte, error) {
				requests++
				<-closed
				time.Sleep(time.Second)
				sendFinished = true
				return false, nil, errors.New("client closed")
			},
			close: func() error {
				closes++
				close(closed)
				return nil
			},
		}
		cleanup := startKeepAlive(client, keepAliveInterval)
		t.Cleanup(func() { _ = cleanup() })

		time.Sleep(keepAliveInterval)
		synctest.Wait()
		if requests != 1 || sendFinished {
			t.Fatalf("Expected a blocked send: requests=%d, finished=%v", requests, sendFinished)
		}
		if err := cleanup(); err != nil {
			t.Fatalf("Cleanup while sending: %v", err)
		}
		if !sendFinished {
			t.Fatal("Cleanup returned before the blocked send finished")
		}
		if closes != 1 {
			t.Errorf("After cleanup: closes=%d; want 1", closes)
		}
	})
}

func TestKeepAliveConcurrentCleanupPreservesCloseError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		closeErr := errors.New("close failed")
		closeStarted := make(chan struct{})
		allowClose := make(chan struct{})
		var closes int
		client := &mockKeepAliveClient{
			sendRequest: func(string, bool, []byte) (bool, []byte, error) {
				t.Error("Sent a keepalive after cleanup started")
				return false, nil, nil
			},
			close: func() error {
				closes++
				close(closeStarted)
				<-allowClose
				return closeErr
			},
		}
		cleanup := startKeepAlive(client, keepAliveInterval)
		t.Cleanup(func() { _ = cleanup() })

		const callers = 4
		results := make(chan error, callers)
		for range callers {
			go func() { results <- cleanup() }()
		}
		<-closeStarted
		close(allowClose)
		for range callers {
			if err := <-results; !errors.Is(err, closeErr) {
				t.Errorf("Concurrent cleanup: got %v, want %v", err, closeErr)
			}
		}
		if err := cleanup(); !errors.Is(err, closeErr) {
			t.Errorf("Repeated cleanup: got %v, want %v", err, closeErr)
		}
		time.Sleep(2 * keepAliveInterval)
		synctest.Wait()
		if closes != 1 {
			t.Errorf("Closed the client %d times, want 1", closes)
		}
	})
}
