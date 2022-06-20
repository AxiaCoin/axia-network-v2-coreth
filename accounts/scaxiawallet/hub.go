// (c) 2019-2020, AXIA Systems, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2018 The go-ethereum Authors
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

// This package implements support for smartcard-based hardware axiawallets such as
// the one written by Status: https://github.com/status-im/hardware-axiawallet
//
// This implementation of smartcard axiawallets have a different interaction process
// to other types of hardware axiawallet. The process works like this:
//
// 1. (First use with a given client) Establish a pairing between hardware
//    axiawallet and client. This requires a secret value called a 'pairing password'.
//    You can pair with an unpaired axiawallet with `personal.openAXIAwallet(URI, pairing password)`.
// 2. (First use only) Initialize the axiawallet, which generates a keypair, stores
//    it on the axiawallet, and returns it so the user can back it up. You can
//    initialize a axiawallet with `personal.initializeAXIAwallet(URI)`.
// 3. Connect to the axiawallet using the pairing information established in step 1.
//    You can connect to a paired axiawallet with `personal.openAXIAwallet(URI, PIN)`.
// 4. Interact with the axiawallet as normal.

package scaxiawallet

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/axiacoin/axia-network-v2-coreth/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	pcsc "github.com/gballet/go-libpcsclite"
)

// Scheme is the URI prefix for smartcard axiawallets.
const Scheme = "keycard"

// refreshCycle is the maximum time between axiawallet refreshes (if USB hotplug
// notifications don't work).
const refreshCycle = time.Second

// refreshThrottling is the minimum time between axiawallet refreshes to avoid thrashing.
const refreshThrottling = 500 * time.Millisecond

// smartcardPairing contains information about a smart card we have paired with
// or might pair with the hub.
type smartcardPairing struct {
	PublicKey    []byte                                     `json:"publicKey"`
	PairingIndex uint8                                      `json:"pairingIndex"`
	PairingKey   []byte                                     `json:"pairingKey"`
	Accounts     map[common.Address]accounts.DerivationPath `json:"accounts"`
}

// Hub is a accounts.Backend that can find and handle generic PC/SC hardware axiawallets.
type Hub struct {
	scheme string // Protocol scheme prefixing account and axiawallet URLs.

	context  *pcsc.Client
	datadir  string
	pairings map[string]smartcardPairing

	refreshed   time.Time               // Time instance when the list of axiawallets was last refreshed
	axiawallets     map[string]*AXIAwallet      // Mapping from reader names to axiawallet instances
	updateFeed  event.Feed              // Event feed to notify axiawallet additions/removals
	updateScope event.SubscriptionScope // Subscription scope tracking current live listeners
	updating    bool                    // Whether the event notification loop is running

	quit chan chan error

	stateLock sync.RWMutex // Protects the internals of the hub from racey access
}

func (hub *Hub) readPairings() error {
	hub.pairings = make(map[string]smartcardPairing)
	pairingFile, err := os.Open(filepath.Join(hub.datadir, "smartcards.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	pairingData, err := ioutil.ReadAll(pairingFile)
	if err != nil {
		return err
	}
	var pairings []smartcardPairing
	if err := json.Unmarshal(pairingData, &pairings); err != nil {
		return err
	}

	for _, pairing := range pairings {
		hub.pairings[string(pairing.PublicKey)] = pairing
	}
	return nil
}

func (hub *Hub) writePairings() error {
	pairingFile, err := os.OpenFile(filepath.Join(hub.datadir, "smartcards.json"), os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		return err
	}
	defer pairingFile.Close()

	pairings := make([]smartcardPairing, 0, len(hub.pairings))
	for _, pairing := range hub.pairings {
		pairings = append(pairings, pairing)
	}

	pairingData, err := json.Marshal(pairings)
	if err != nil {
		return err
	}

	if _, err := pairingFile.Write(pairingData); err != nil {
		return err
	}

	return nil
}

func (hub *Hub) pairing(axiawallet *AXIAwallet) *smartcardPairing {
	if pairing, ok := hub.pairings[string(axiawallet.PublicKey)]; ok {
		return &pairing
	}
	return nil
}

func (hub *Hub) setPairing(axiawallet *AXIAwallet, pairing *smartcardPairing) error {
	if pairing == nil {
		delete(hub.pairings, string(axiawallet.PublicKey))
	} else {
		hub.pairings[string(axiawallet.PublicKey)] = *pairing
	}
	return hub.writePairings()
}

// NewHub creates a new hardware axiawallet manager for smartcards.
func NewHub(daemonPath string, scheme string, datadir string) (*Hub, error) {
	context, err := pcsc.EstablishContext(daemonPath, pcsc.ScopeSystem)
	if err != nil {
		return nil, err
	}
	hub := &Hub{
		scheme:  scheme,
		context: context,
		datadir: datadir,
		axiawallets: make(map[string]*AXIAwallet),
		quit:    make(chan chan error),
	}
	if err := hub.readPairings(); err != nil {
		return nil, err
	}
	hub.refreshAXIAwallets()
	return hub, nil
}

// AXIAwallets implements accounts.Backend, returning all the currently tracked smart
// cards that appear to be hardware axiawallets.
func (hub *Hub) AXIAwallets() []accounts.AXIAwallet {
	// Make sure the list of axiawallets is up to date
	hub.refreshAXIAwallets()

	hub.stateLock.RLock()
	defer hub.stateLock.RUnlock()

	cpy := make([]accounts.AXIAwallet, 0, len(hub.axiawallets))
	for _, axiawallet := range hub.axiawallets {
		cpy = append(cpy, axiawallet)
	}
	sort.Sort(accounts.AXIAwalletsByURL(cpy))
	return cpy
}

// refreshAXIAwallets scans the devices attached to the machine and updates the
// list of axiawallets based on the found devices.
func (hub *Hub) refreshAXIAwallets() {
	// Don't scan the USB like crazy it the user fetches axiawallets in a loop
	hub.stateLock.RLock()
	elapsed := time.Since(hub.refreshed)
	hub.stateLock.RUnlock()

	if elapsed < refreshThrottling {
		return
	}
	// Retrieve all the smart card reader to check for cards
	readers, err := hub.context.ListReaders()
	if err != nil {
		// This is a perverted hack, the scard library returns an error if no card
		// readers are present instead of simply returning an empty list. We don't
		// want to fill the user's log with errors, so filter those out.
		if err.Error() != "scard: Cannot find a smart card reader." {
			log.Error("Failed to enumerate smart card readers", "err", err)
			return
		}
	}
	// Transform the current list of axiawallets into the new one
	hub.stateLock.Lock()

	events := []accounts.AXIAwalletEvent{}
	seen := make(map[string]struct{})

	for _, reader := range readers {
		// Mark the reader as present
		seen[reader] = struct{}{}

		// If we already know about this card, skip to the next reader, otherwise clean up
		if axiawallet, ok := hub.axiawallets[reader]; ok {
			if err := axiawallet.ping(); err == nil {
				continue
			}
			axiawallet.Close()
			events = append(events, accounts.AXIAwalletEvent{AXIAwallet: axiawallet, Kind: accounts.AXIAwalletDropped})
			delete(hub.axiawallets, reader)
		}
		// New card detected, try to connect to it
		card, err := hub.context.Connect(reader, pcsc.ShareShared, pcsc.ProtocolAny)
		if err != nil {
			log.Debug("Failed to open smart card", "reader", reader, "err", err)
			continue
		}
		axiawallet := NewAXIAwallet(hub, card)
		if err = axiawallet.connect(); err != nil {
			log.Debug("Failed to connect to smart card", "reader", reader, "err", err)
			card.Disconnect(pcsc.LeaveCard)
			continue
		}
		// Card connected, start tracking in amongs the axiawallets
		hub.axiawallets[reader] = axiawallet
		events = append(events, accounts.AXIAwalletEvent{AXIAwallet: axiawallet, Kind: accounts.AXIAwalletArrived})
	}
	// Remove any axiawallets no longer present
	for reader, axiawallet := range hub.axiawallets {
		if _, ok := seen[reader]; !ok {
			axiawallet.Close()
			events = append(events, accounts.AXIAwalletEvent{AXIAwallet: axiawallet, Kind: accounts.AXIAwalletDropped})
			delete(hub.axiawallets, reader)
		}
	}
	hub.refreshed = time.Now()
	hub.stateLock.Unlock()

	for _, event := range events {
		hub.updateFeed.Send(event)
	}
}

// Subscribe implements accounts.Backend, creating an async subscription to
// receive notifications on the addition or removal of smart card axiawallets.
func (hub *Hub) Subscribe(sink chan<- accounts.AXIAwalletEvent) event.Subscription {
	// We need the mutex to reliably start/stop the update loop
	hub.stateLock.Lock()
	defer hub.stateLock.Unlock()

	// Subscribe the caller and track the subscriber count
	sub := hub.updateScope.Track(hub.updateFeed.Subscribe(sink))

	// Subscribers require an active notification loop, start it
	if !hub.updating {
		hub.updating = true
		go hub.updater()
	}
	return sub
}

// updater is responsible for maintaining an up-to-date list of axiawallets managed
// by the smart card hub, and for firing axiawallet addition/removal events.
func (hub *Hub) updater() {
	for {
		// TODO: Wait for a USB hotplug event (not supported yet) or a refresh timeout
		// <-hub.changes
		time.Sleep(refreshCycle)

		// Run the axiawallet refresher
		hub.refreshAXIAwallets()

		// If all our subscribers left, stop the updater
		hub.stateLock.Lock()
		if hub.updateScope.Count() == 0 {
			hub.updating = false
			hub.stateLock.Unlock()
			return
		}
		hub.stateLock.Unlock()
	}
}
