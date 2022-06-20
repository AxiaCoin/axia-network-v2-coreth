// (c) 2019-2020, Ava Labs, Inc.
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

// Package accounts implements high level Ethereum account management.
package accounts

import (
	"fmt"
	"math/big"

	"github.com/axiacoin/axia-network-v2-coreth/core/types"
	"github.com/axiacoin/axia-network-v2-coreth/interfaces"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"golang.org/x/crypto/sha3"
)

// Account represents an Ethereum account located at a specific location defined
// by the optional URL field.
type Account struct {
	Address common.Address `json:"address"` // Ethereum account address derived from the key
	URL     URL            `json:"url"`     // Optional resource locator within a backend
}

const (
	MimetypeDataWithValidator = "data/validator"
	MimetypeTypedData         = "data/typed"
	MimetypeClique            = "application/x-clique-header"
	MimetypeTextPlain         = "text/plain"
)

// AXIAwallet represents a software or hardware axiawallet that might contain one or more
// accounts (derived from the same seed).
type AXIAwallet interface {
	// URL retrieves the canonical path under which this axiawallet is reachable. It is
	// used by upper layers to define a sorting order over all axiawallets from multiple
	// backends.
	URL() URL

	// Status returns a textual status to aid the user in the current state of the
	// axiawallet. It also returns an error indicating any failure the axiawallet might have
	// encountered.
	Status() (string, error)

	// Open initializes access to a axiawallet instance. It is not meant to unlock or
	// decrypt account keys, rather simply to establish a connection to hardware
	// axiawallets and/or to access derivation seeds.
	//
	// The passphrase parameter may or may not be used by the implementation of a
	// particular axiawallet instance. The reason there is no passwordless open method
	// is to strive towards a uniform axiawallet handling, oblivious to the different
	// backend providers.
	//
	// Please note, if you open a axiawallet, you must close it to release any allocated
	// resources (especially important when working with hardware axiawallets).
	Open(passphrase string) error

	// Close releases any resources held by an open axiawallet instance.
	Close() error

	// Accounts retrieves the list of signing accounts the axiawallet is currently aware
	// of. For hierarchical deterministic axiawallets, the list will not be exhaustive,
	// rather only contain the accounts explicitly pinned during account derivation.
	Accounts() []Account

	// Contains returns whether an account is part of this particular axiawallet or not.
	Contains(account Account) bool

	// Derive attempts to explicitly derive a hierarchical deterministic account at
	// the specified derivation path. If requested, the derived account will be added
	// to the axiawallet's tracked account list.
	Derive(path DerivationPath, pin bool) (Account, error)

	// SelfDerive sets a base account derivation path from which the axiawallet attempts
	// to discover non zero accounts and automatically add them to list of tracked
	// accounts.
	//
	// Note, self derivation will increment the last component of the specified path
	// opposed to descending into a child path to allow discovering accounts starting
	// from non zero components.
	//
	// Some hardware axiawallets switched derivation paths through their evolution, so
	// this method supports providing multiple bases to discover old user accounts
	// too. Only the last base will be used to derive the next empty account.
	//
	// You can disable automatic account discovery by calling SelfDerive with a nil
	// chain state reader.
	SelfDerive(bases []DerivationPath, chain interfaces.ChainStateReader)

	// SignData requests the axiawallet to sign the hash of the given data
	// It looks up the account specified either solely via its address contained within,
	// or optionally with the aid of any location metadata from the embedded URL field.
	//
	// If the axiawallet requires additional authentication to sign the request (e.g.
	// a password to decrypt the account, or a PIN code to verify the transaction),
	// an AuthNeededError instance will be returned, containing infos for the user
	// about which fields or actions are needed. The user may retry by providing
	// the needed details via SignDataWithPassphrase, or by other means (e.g. unlock
	// the account in a keystore).
	SignData(account Account, mimeType string, data []byte) ([]byte, error)

	// SignDataWithPassphrase is identical to SignData, but also takes a password
	// NOTE: there's a chance that an erroneous call might mistake the two strings, and
	// supply password in the mimetype field, or vice versa. Thus, an implementation
	// should never echo the mimetype or return the mimetype in the error-response
	SignDataWithPassphrase(account Account, passphrase, mimeType string, data []byte) ([]byte, error)

	// SignText requests the axiawallet to sign the hash of a given piece of data, prefixed
	// by the Ethereum prefix scheme
	// It looks up the account specified either solely via its address contained within,
	// or optionally with the aid of any location metadata from the embedded URL field.
	//
	// If the axiawallet requires additional authentication to sign the request (e.g.
	// a password to decrypt the account, or a PIN code to verify the transaction),
	// an AuthNeededError instance will be returned, containing infos for the user
	// about which fields or actions are needed. The user may retry by providing
	// the needed details via SignTextWithPassphrase, or by other means (e.g. unlock
	// the account in a keystore).
	//
	// This method should return the signature in 'canonical' format, with v 0 or 1.
	SignText(account Account, text []byte) ([]byte, error)

	// SignTextWithPassphrase is identical to Signtext, but also takes a password
	SignTextWithPassphrase(account Account, passphrase string, hash []byte) ([]byte, error)

	// SignTx requests the axiawallet to sign the given transaction.
	//
	// It looks up the account specified either solely via its address contained within,
	// or optionally with the aid of any location metadata from the embedded URL field.
	//
	// If the axiawallet requires additional authentication to sign the request (e.g.
	// a password to decrypt the account, or a PIN code to verify the transaction),
	// an AuthNeededError instance will be returned, containing infos for the user
	// about which fields or actions are needed. The user may retry by providing
	// the needed details via SignTxWithPassphrase, or by other means (e.g. unlock
	// the account in a keystore).
	SignTx(account Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)

	// SignTxWithPassphrase is identical to SignTx, but also takes a password
	SignTxWithPassphrase(account Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)
}

// Backend is a "axiawallet provider" that may contain a batch of accounts they can
// sign transactions with and upon request, do so.
type Backend interface {
	// AXIAwallets retrieves the list of axiawallets the backend is currently aware of.
	//
	// The returned axiawallets are not opened by default. For software HD axiawallets this
	// means that no base seeds are decrypted, and for hardware axiawallets that no actual
	// connection is established.
	//
	// The resulting axiawallet list will be sorted alphabetically based on its internal
	// URL assigned by the backend. Since axiawallets (especially hardware) may come and
	// go, the same axiawallet might appear at a different positions in the list during
	// subsequent retrievals.
	AXIAwallets() []AXIAwallet

	// Subscribe creates an async subscription to receive notifications when the
	// backend detects the arrival or departure of a axiawallet.
	Subscribe(sink chan<- AXIAwalletEvent) event.Subscription
}

// TextHash is a helper function that calculates a hash for the given message that can be
// safely used to calculate a signature from.
//
// The hash is calculated as
//   keccak256("\x19Ethereum Signed Message:\n"${message length}${message}).
//
// This gives context to the signed message and prevents signing of transactions.
func TextHash(data []byte) []byte {
	hash, _ := TextAndHash(data)
	return hash
}

// TextAndHash is a helper function that calculates a hash for the given message that can be
// safely used to calculate a signature from.
//
// The hash is calculated as
//   keccak256("\x19Ethereum Signed Message:\n"${message length}${message}).
//
// This gives context to the signed message and prevents signing of transactions.
func TextAndHash(data []byte) ([]byte, string) {
	msg := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(data), string(data))
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(msg))
	return hasher.Sum(nil), msg
}

// AXIAwalletEventType represents the different event types that can be fired by
// the axiawallet subscription subsystem.
type AXIAwalletEventType int

const (
	// AXIAwalletArrived is fired when a new axiawallet is detected either via USB or via
	// a filesystem event in the keystore.
	AXIAwalletArrived AXIAwalletEventType = iota

	// AXIAwalletOpened is fired when a axiawallet is successfully opened with the purpose
	// of starting any background processes such as automatic key derivation.
	AXIAwalletOpened

	// AXIAwalletDropped
	AXIAwalletDropped
)

// AXIAwalletEvent is an event fired by an account backend when a axiawallet arrival or
// departure is detected.
type AXIAwalletEvent struct {
	AXIAwallet AXIAwallet          // AXIAwallet instance arrived or departed
	Kind   AXIAwalletEventType // Event type that happened in the system
}
