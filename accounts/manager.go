// (c) 2019-2020, Axia Systems, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package accounts

import (
	"reflect"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
)

// managerSubBufferSize determines how many incoming axiawallet events
// the manager will buffer in its channel.
const managerSubBufferSize = 50

// Config contains the settings of the global account manager.
//
// TODO(rjl493456442, karalabe, holiman): Get rid of this when account management
// is removed in favor of Clef.
type Config struct {
	InsecureUnlockAllowed bool // Whether account unlocking in insecure environment is allowed
}

// newBackendEvent lets the manager know it should
// track the given backend for axiawallet updates.
type newBackendEvent struct {
	backend   Backend
	processed chan struct{} // Informs event emitter that backend has been integrated
}

// Manager is an overarching account manager that can communicate with various
// backends for signing transactions.
type Manager struct {
	config      *Config                    // Global account manager configurations
	backends    map[reflect.Type][]Backend // Index of backends currently registered
	updaters    []event.Subscription       // AxiaWallet update subscriptions for all backends
	updates     chan AxiaWalletEvent           // Subscription sink for backend axiawallet changes
	newBackends chan newBackendEvent       // Incoming backends to be tracked by the manager
	axiawallets     []AxiaWallet                   // Cache of all axiawallets from all registered backends

	feed event.Feed // AxiaWallet feed notifying of arrivals/departures

	quit chan chan error
	term chan struct{} // Channel is closed upon termination of the update loop
	lock sync.RWMutex
}

// NewManager creates a generic account manager to sign transaction via various
// supported backends.
func NewManager(config *Config, backends ...Backend) *Manager {
	// Retrieve the initial list of axiawallets from the backends and sort by URL
	var axiawallets []AxiaWallet
	for _, backend := range backends {
		axiawallets = merge(axiawallets, backend.AxiaWallets()...)
	}
	// Subscribe to axiawallet notifications from all backends
	updates := make(chan AxiaWalletEvent, managerSubBufferSize)

	subs := make([]event.Subscription, len(backends))
	for i, backend := range backends {
		subs[i] = backend.Subscribe(updates)
	}
	// Assemble the account manager and return
	am := &Manager{
		config:      config,
		backends:    make(map[reflect.Type][]Backend),
		updaters:    subs,
		updates:     updates,
		newBackends: make(chan newBackendEvent),
		axiawallets:     axiawallets,
		quit:        make(chan chan error),
		term:        make(chan struct{}),
	}
	for _, backend := range backends {
		kind := reflect.TypeOf(backend)
		am.backends[kind] = append(am.backends[kind], backend)
	}
	go am.update()

	return am
}

// Close terminates the account manager's internal notification processes.
func (am *Manager) Close() error {
	errc := make(chan error)
	am.quit <- errc
	return <-errc
}

// Config returns the configuration of account manager.
func (am *Manager) Config() *Config {
	return am.config
}

// AddBackend starts the tracking of an additional backend for axiawallet updates.
// cmd/geth assumes once this func returns the backends have been already integrated.
func (am *Manager) AddBackend(backend Backend) {
	done := make(chan struct{})
	am.newBackends <- newBackendEvent{backend, done}
	<-done
}

// update is the axiawallet event loop listening for notifications from the backends
// and updating the cache of axiawallets.
func (am *Manager) update() {
	// Close all subscriptions when the manager terminates
	defer func() {
		am.lock.Lock()
		for _, sub := range am.updaters {
			sub.Unsubscribe()
		}
		am.updaters = nil
		am.lock.Unlock()
	}()

	// Loop until termination
	for {
		select {
		case event := <-am.updates:
			// AxiaWallet event arrived, update local cache
			am.lock.Lock()
			switch event.Kind {
			case AxiaWalletArrived:
				am.axiawallets = merge(am.axiawallets, event.AxiaWallet)
			case AxiaWalletDropped:
				am.axiawallets = drop(am.axiawallets, event.AxiaWallet)
			}
			am.lock.Unlock()

			// Notify any listeners of the event
			am.feed.Send(event)
		case event := <-am.newBackends:
			am.lock.Lock()
			// Update caches
			backend := event.backend
			am.axiawallets = merge(am.axiawallets, backend.AxiaWallets()...)
			am.updaters = append(am.updaters, backend.Subscribe(am.updates))
			kind := reflect.TypeOf(backend)
			am.backends[kind] = append(am.backends[kind], backend)
			am.lock.Unlock()
			close(event.processed)
		case errc := <-am.quit:
			// Manager terminating, return
			errc <- nil
			// Signals event emitters the loop is not receiving values
			// to prevent them from getting stuck.
			close(am.term)
			return
		}
	}
}

// Backends retrieves the backend(s) with the given type from the account manager.
func (am *Manager) Backends(kind reflect.Type) []Backend {
	am.lock.RLock()
	defer am.lock.RUnlock()

	return am.backends[kind]
}

// AxiaWallets returns all signer accounts registered under this account manager.
func (am *Manager) AxiaWallets() []AxiaWallet {
	am.lock.RLock()
	defer am.lock.RUnlock()

	return am.axiawalletsNoLock()
}

// axiawalletsNoLock returns all registered axiawallets. Callers must hold am.lock.
func (am *Manager) axiawalletsNoLock() []AxiaWallet {
	cpy := make([]AxiaWallet, len(am.axiawallets))
	copy(cpy, am.axiawallets)
	return cpy
}

// AxiaWallet retrieves the axiawallet associated with a particular URL.
func (am *Manager) AxiaWallet(url string) (AxiaWallet, error) {
	am.lock.RLock()
	defer am.lock.RUnlock()

	parsed, err := parseURL(url)
	if err != nil {
		return nil, err
	}
	for _, axiawallet := range am.axiawalletsNoLock() {
		if axiawallet.URL() == parsed {
			return axiawallet, nil
		}
	}
	return nil, ErrUnknownAxiaWallet
}

// Accounts returns all account addresses of all axiawallets within the account manager
func (am *Manager) Accounts() []common.Address {
	am.lock.RLock()
	defer am.lock.RUnlock()

	addresses := make([]common.Address, 0) // return [] instead of nil if empty
	for _, axiawallet := range am.axiawallets {
		for _, account := range axiawallet.Accounts() {
			addresses = append(addresses, account.Address)
		}
	}
	return addresses
}

// Find attempts to locate the axiawallet corresponding to a specific account. Since
// accounts can be dynamically added to and removed from axiawallets, this method has
// a linear runtime in the number of axiawallets.
func (am *Manager) Find(account Account) (AxiaWallet, error) {
	am.lock.RLock()
	defer am.lock.RUnlock()

	for _, axiawallet := range am.axiawallets {
		if axiawallet.Contains(account) {
			return axiawallet, nil
		}
	}
	return nil, ErrUnknownAccount
}

// Subscribe creates an async subscription to receive notifications when the
// manager detects the arrival or departure of a axiawallet from any of its backends.
func (am *Manager) Subscribe(sink chan<- AxiaWalletEvent) event.Subscription {
	return am.feed.Subscribe(sink)
}

// merge is a sorted analogue of append for axiawallets, where the ordering of the
// origin list is preserved by inserting new axiawallets at the correct position.
//
// The original slice is assumed to be already sorted by URL.
func merge(slice []AxiaWallet, axiawallets ...AxiaWallet) []AxiaWallet {
	for _, axiawallet := range axiawallets {
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(axiawallet.URL()) >= 0 })
		if n == len(slice) {
			slice = append(slice, axiawallet)
			continue
		}
		slice = append(slice[:n], append([]AxiaWallet{axiawallet}, slice[n:]...)...)
	}
	return slice
}

// drop is the couterpart of merge, which looks up axiawallets from within the sorted
// cache and removes the ones specified.
func drop(slice []AxiaWallet, axiawallets ...AxiaWallet) []AxiaWallet {
	for _, axiawallet := range axiawallets {
		n := sort.Search(len(slice), func(i int) bool { return slice[i].URL().Cmp(axiawallet.URL()) >= 0 })
		if n == len(slice) {
			// AxiaWallet not found, may happen during startup
			continue
		}
		slice = append(slice[:n], slice[n+1:]...)
	}
	return slice
}
