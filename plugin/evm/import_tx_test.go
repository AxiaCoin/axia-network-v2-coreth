// (c) 2019-2020, Axia Systems, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/axiacoin/axia-network-v2-coreth/params"
	"github.com/ethereum/go-ethereum/common"

	"github.com/axiacoin/axia-network-v2/chains/atomic"
	"github.com/axiacoin/axia-network-v2/ids"
	"github.com/axiacoin/axia-network-v2/utils/constants"
	"github.com/axiacoin/axia-network-v2/utils/crypto"
	"github.com/axiacoin/axia-network-v2/vms/components/axc"
	"github.com/axiacoin/axia-network-v2/vms/secp256k1fx"
)

// createImportTxOptions adds a UTXO to shared memory and generates a list of import transactions sending this UTXO
// to each of the three test keys (conflicting transactions)
func createImportTxOptions(t *testing.T, vm *VM, sharedMemory *atomic.Memory) []*Tx {
	utxo := &axc.UTXO{
		UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
		Asset:  axc.Asset{ID: vm.ctx.AXCAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(50000000),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	swapChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.SwapChainID)
	inputID := utxo.InputID()
	if err := swapChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTxs := make([]*Tx, 0, 3)
	for _, ethAddr := range testEthAddrs {
		importTx, err := vm.newImportTx(vm.ctx.SwapChainID, ethAddr, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
		if err != nil {
			t.Fatal(err)
		}
		importTxs = append(importTxs, importTx)
	}

	return importTxs
}

func TestImportTxVerify(t *testing.T) {
	ctx := NewContext()

	var importAmount uint64 = 10000000
	txID := ids.GenerateTestID()
	importTx := &UnsignedImportTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		SourceChain:  ctx.SwapChainID,
		ImportedInputs: []*axc.TransferableInput{
			{
				UTXOID: axc.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(0),
				},
				Asset: axc.Asset{ID: ctx.AXCAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: importAmount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
			{
				UTXOID: axc.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(1),
				},
				Asset: axc.Asset{ID: ctx.AXCAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: importAmount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		Outs: []EVMOutput{
			{
				Address: testEthAddrs[0],
				Amount:  importAmount - params.AxiaAtomicTxFee,
				AssetID: ctx.AXCAssetID,
			},
			{
				Address: testEthAddrs[1],
				Amount:  importAmount,
				AssetID: ctx.AXCAssetID,
			},
		},
	}

	// // Sort the inputs and outputs to ensure the transaction is canonical
	axc.SortTransferableInputs(importTx.ImportedInputs)
	SortEVMOutputs(importTx.Outs)

	tests := map[string]atomicTxVerifyTest{
		"nil tx": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				var importTx *UnsignedImportTx
				return importTx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: errNilTx.Error(),
		},
		"valid import tx": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				return importTx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "", // Expect this transaction to be valid
		},
		"invalid network ID": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.NetworkID++
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: errWrongNetworkID.Error(),
		},
		"invalid blockchain ID": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.BlockchainID = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: errWrongBlockchainID.Error(),
		},
		"Core-chain source before AP5": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = constants.PlatformChainID
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: errWrongChainID.Error(),
		},
		"Core-chain source after AP5": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = constants.PlatformChainID
				return &tx
			},
			ctx:   ctx,
			rules: apricotRulesPhase5,
		},
		"invalid source chain ID": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.SourceChain = ids.GenerateTestID()
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase5,
			expectedErr: errWrongChainID.Error(),
		},
		"no inputs": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: errNoImportInputs.Error(),
		},
		"inputs sorted incorrectly": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*axc.TransferableInput{
					tx.ImportedInputs[1],
					tx.ImportedInputs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: errInputsNotSortedUnique.Error(),
		},
		"invalid input": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.ImportedInputs = []*axc.TransferableInput{
					tx.ImportedInputs[0],
					nil,
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "atomic input failed verification",
		},
		"unsorted outputs phase 0 passes verification": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "",
		},
		"non-unique outputs phase 0 passes verification": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "",
		},
		"unsorted outputs phase 1 fails verification": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase1,
			expectedErr: errOutputsNotSorted.Error(),
		},
		"non-unique outputs phase 1 passes verification": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase1,
			expectedErr: "",
		},
		"outputs not sorted and unique phase 2 fails verification": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []EVMOutput{
					tx.Outs[0],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase2,
			expectedErr: errOutputsNotSortedUnique.Error(),
		},
		"outputs not sorted phase 2 fails verification": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []EVMOutput{
					tx.Outs[1],
					tx.Outs[0],
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase2,
			expectedErr: errOutputsNotSortedUnique.Error(),
		},
		"invalid EVMOutput fails verification": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = []EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  0,
						AssetID: testAxcAssetID,
					},
				}
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase0,
			expectedErr: "EVM Output failed verification",
		},
		"no outputs apricot phase 3": {
			generate: func(t *testing.T) UnsignedAtomicTx {
				tx := *importTx
				tx.Outs = nil
				return &tx
			},
			ctx:         ctx,
			rules:       apricotRulesPhase3,
			expectedErr: errNoEVMOutputs.Error(),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxVerifyTest(t, test)
		})
	}
}

func TestNewImportTx(t *testing.T) {
	importAmount := uint64(5000000)
	// createNewImportAXCTx adds a UTXO to shared memory and then constructs a new import transaction
	// and checks that it has the correct fee for the base fee that has been used
	createNewImportAXCTx := func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
		txID := ids.GenerateTestID()
		_, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AXCAssetID, importAmount, testShortIDAddrs[0])
		if err != nil {
			t.Fatal(err)
		}

		tx, err := vm.newImportTx(vm.ctx.SwapChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
		if err != nil {
			t.Fatal(err)
		}
		importTx := tx.UnsignedAtomicTx
		var actualFee uint64
		actualAXCBurned, err := importTx.Burned(vm.ctx.AXCAssetID)
		if err != nil {
			t.Fatal(err)
		}
		rules := vm.currentRules()
		switch {
		case rules.IsApricotPhase3:
			actualCost, err := importTx.GasUsed(rules.IsApricotPhase5)
			if err != nil {
				t.Fatal(err)
			}
			actualFee, err = calculateDynamicFee(actualCost, initialBaseFee)
			if err != nil {
				t.Fatal(err)
			}
		case rules.IsApricotPhase2:
			actualFee = 1000000
		default:
			actualFee = 0
		}

		if actualAXCBurned != actualFee {
			t.Fatalf("AXC burned (%d) != actual fee (%d)", actualAXCBurned, actualFee)
		}

		return tx
	}
	checkState := func(t *testing.T, vm *VM) {
		txs := vm.LastAcceptedBlockInternal().(*Block).atomicTxs
		if len(txs) != 1 {
			t.Fatalf("Expected one import tx to be in the last accepted block, but found %d", len(txs))
		}

		tx := txs[0]
		actualAXCBurned, err := tx.UnsignedAtomicTx.Burned(vm.ctx.AXCAssetID)
		if err != nil {
			t.Fatal(err)
		}

		// Ensure that the UTXO has been removed from shared memory within Accept
		addrSet := ids.ShortSet{}
		addrSet.Add(testShortIDAddrs[0])
		utxos, _, _, err := vm.GetAtomicUTXOs(vm.ctx.SwapChainID, addrSet, ids.ShortEmpty, ids.Empty, -1)
		if err != nil {
			t.Fatal(err)
		}
		if len(utxos) != 0 {
			t.Fatalf("Expected to find 0 UTXOs after accepting import transaction, but found %d", len(utxos))
		}

		// Ensure that the call to EVMStateTransfer correctly updates the balance of [addr]
		sdb, err := vm.chain.CurrentState()
		if err != nil {
			t.Fatal(err)
		}

		expectedRemainingBalance := new(big.Int).Mul(new(big.Int).SetUint64(importAmount-actualAXCBurned), x2cRate)
		addr := GetEthAddress(testKeys[0])
		if actualBalance := sdb.GetBalance(addr); actualBalance.Cmp(expectedRemainingBalance) != 0 {
			t.Fatalf("address remaining balance %s equal %s not %s", addr.String(), actualBalance, expectedRemainingBalance)
		}

	}
	tests2 := map[string]atomicTxTest{
		"apricot phase 0": {
			setup:       createNewImportAXCTx,
			checkState:  checkState,
			genesisJSON: genesisJSONApricotPhase0,
		},
		"apricot phase 1": {
			setup:       createNewImportAXCTx,
			checkState:  checkState,
			genesisJSON: genesisJSONApricotPhase1,
		},
		"apricot phase 2": {
			setup:       createNewImportAXCTx,
			checkState:  checkState,
			genesisJSON: genesisJSONApricotPhase2,
		},
		"apricot phase 3": {
			setup:       createNewImportAXCTx,
			checkState:  checkState,
			genesisJSON: genesisJSONApricotPhase3,
		},
	}

	for name, test := range tests2 {
		t.Run(name, func(t *testing.T) {
			executeTxTest(t, test)
		})
	}
}

// Note: this is a brittle test to ensure that the gas cost of a transaction does
// not change
func TestImportTxGasCost(t *testing.T) {
	axcAssetID := ids.GenerateTestID()
	antAssetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	swapChainID := ids.GenerateTestID()
	networkID := uint32(5)
	importAmount := uint64(5000000)

	tests := map[string]struct {
		UnsignedImportTx *UnsignedImportTx
		Keys             [][]*crypto.PrivateKeySECP256K1R

		ExpectedGasUsed uint64
		ExpectedFee     uint64
		BaseFee         *big.Int
		FixedFee        bool
	}{
		"simple import": {
			UnsignedImportTx: &UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  swapChainID,
				ImportedInputs: []*axc.TransferableInput{{
					UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  axc.Asset{ID: axcAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: axcAssetID,
				}},
			},
			Keys:            [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}},
			ExpectedGasUsed: 1230,
			ExpectedFee:     30750,
			BaseFee:         big.NewInt(25 * params.GWei),
		},
		"simple import 1wei": {
			UnsignedImportTx: &UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  swapChainID,
				ImportedInputs: []*axc.TransferableInput{{
					UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  axc.Asset{ID: axcAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: axcAssetID,
				}},
			},
			Keys:            [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}},
			ExpectedGasUsed: 1230,
			ExpectedFee:     1,
			BaseFee:         big.NewInt(1),
		},
		"simple import 1wei + fixed fee": {
			UnsignedImportTx: &UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  swapChainID,
				ImportedInputs: []*axc.TransferableInput{{
					UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  axc.Asset{ID: axcAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0}},
					},
				}},
				Outs: []EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: axcAssetID,
				}},
			},
			Keys:            [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}},
			ExpectedGasUsed: 11230,
			ExpectedFee:     1,
			BaseFee:         big.NewInt(1),
			FixedFee:        true,
		},
		"simple ANT import": {
			UnsignedImportTx: &UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  swapChainID,
				ImportedInputs: []*axc.TransferableInput{
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: antAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				},
				Outs: []EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  importAmount,
						AssetID: antAssetID,
					},
				},
			},
			Keys:            [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}, {testKeys[0]}},
			ExpectedGasUsed: 2318,
			ExpectedFee:     57950,
			BaseFee:         big.NewInt(25 * params.GWei),
		},
		"complex ANT import": {
			UnsignedImportTx: &UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  swapChainID,
				ImportedInputs: []*axc.TransferableInput{
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: antAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				},
				Outs: []EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  importAmount,
						AssetID: axcAssetID,
					},
					{
						Address: testEthAddrs[0],
						Amount:  importAmount,
						AssetID: antAssetID,
					},
				},
			},
			Keys:            [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}, {testKeys[0]}},
			ExpectedGasUsed: 2378,
			ExpectedFee:     59450,
			BaseFee:         big.NewInt(25 * params.GWei),
		},
		"multisig import": {
			UnsignedImportTx: &UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  swapChainID,
				ImportedInputs: []*axc.TransferableInput{{
					UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
					Asset:  axc.Asset{ID: axcAssetID},
					In: &secp256k1fx.TransferInput{
						Amt:   importAmount,
						Input: secp256k1fx.Input{SigIndices: []uint32{0, 1}},
					},
				}},
				Outs: []EVMOutput{{
					Address: testEthAddrs[0],
					Amount:  importAmount,
					AssetID: axcAssetID,
				}},
			},
			Keys:            [][]*crypto.PrivateKeySECP256K1R{{testKeys[0], testKeys[1]}},
			ExpectedGasUsed: 2234,
			ExpectedFee:     55850,
			BaseFee:         big.NewInt(25 * params.GWei),
		},
		"large import": {
			UnsignedImportTx: &UnsignedImportTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				SourceChain:  swapChainID,
				ImportedInputs: []*axc.TransferableInput{
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
					{
						UTXOID: axc.UTXOID{TxID: ids.GenerateTestID()},
						Asset:  axc.Asset{ID: axcAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   importAmount,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					},
				},
				Outs: []EVMOutput{
					{
						Address: testEthAddrs[0],
						Amount:  importAmount * 10,
						AssetID: axcAssetID,
					},
				},
			},
			Keys: [][]*crypto.PrivateKeySECP256K1R{
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
				{testKeys[0]},
			},
			ExpectedGasUsed: 11022,
			ExpectedFee:     275550,
			BaseFee:         big.NewInt(25 * params.GWei),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tx := &Tx{UnsignedAtomicTx: test.UnsignedImportTx}

			// Sign with the correct key
			if err := tx.Sign(Codec, test.Keys); err != nil {
				t.Fatal(err)
			}

			gasUsed, err := tx.GasUsed(test.FixedFee)
			if err != nil {
				t.Fatal(err)
			}
			if gasUsed != test.ExpectedGasUsed {
				t.Fatalf("Expected gasUsed to be %d, but found %d", test.ExpectedGasUsed, gasUsed)
			}

			fee, err := calculateDynamicFee(gasUsed, test.BaseFee)
			if err != nil {
				t.Fatal(err)
			}
			if fee != test.ExpectedFee {
				t.Fatalf("Expected fee to be %d, but found %d", test.ExpectedFee, fee)
			}
		})
	}
}

func TestImportTxSemanticVerify(t *testing.T) {
	tests := map[string]atomicTxTest{
		"UTXO not present during bootstrapping": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: axc.UTXOID{
							TxID: ids.GenerateTestID(),
						},
						Asset: axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			bootstrapping: true,
		},
		"UTXO not present": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: axc.UTXOID{
							TxID: ids.GenerateTestID(),
						},
						Asset: axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "failed to fetch import UTXOs from",
		},
		"garbage UTXO": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				utxoID := axc.UTXOID{TxID: ids.GenerateTestID()}
				swapChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.SwapChainID)
				inputID := utxoID.InputID()
				if err := swapChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
					Key:   inputID[:],
					Value: []byte("hey there"),
					Traits: [][]byte{
						testShortIDAddrs[0].Bytes(),
					},
				}}}}); err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxoID,
						Asset:  axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "failed to unmarshal UTXO",
		},
		"UTXO AssetID mismatch": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				expectedAssetID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, expectedAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: vm.ctx.AXCAssetID}, // Use a different assetID then the actual UTXO
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: errAssetIDMismatch.Error(),
		},
		"insufficient AXC funds": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AXCAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  2, // Produce more output than is consumed by the transaction
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx flow check failed due to",
		},
		"insufficient non-AXC funds": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				assetID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, assetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: assetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  2, // Produce more output than is consumed by the transaction
						AssetID: assetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx flow check failed due to",
		},
		"no signatures": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AXCAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				if err := tx.Sign(vm.codec, nil); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx contained mismatched number of inputs/credentials",
		},
		"incorrect signature": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AXCAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				// Sign the transaction with the incorrect key
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[1]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			semanticVerifyErr: "import tx transfer failed verification",
		},
		"non-unique EVM Outputs": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AXCAssetID, 2, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   2,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{
						{
							Address: testEthAddrs[0],
							Amount:  1,
							AssetID: vm.ctx.AXCAssetID,
						},
						{
							Address: testEthAddrs[0],
							Amount:  1,
							AssetID: vm.ctx.AXCAssetID,
						},
					},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			genesisJSON:       genesisJSONApricotPhase3,
			semanticVerifyErr: errOutputsNotSortedUnique.Error(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxTest(t, test)
		})
	}
}

func TestImportTxEVMStateTransfer(t *testing.T) {
	assetID := ids.GenerateTestID()
	tests := map[string]atomicTxTest{
		"AXC UTXO": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, vm.ctx.AXCAssetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: vm.ctx.AXCAssetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: vm.ctx.AXCAssetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			checkState: func(t *testing.T, vm *VM) {
				lastAcceptedBlock := vm.LastAcceptedBlockInternal().(*Block)

				sdb, err := vm.chain.BlockState(lastAcceptedBlock.ethBlock)
				if err != nil {
					t.Fatal(err)
				}

				axcBalance := sdb.GetBalance(testEthAddrs[0])
				if axcBalance.Cmp(x2cRate) != 0 {
					t.Fatalf("Expected AXC balance to be %d, found balance: %d", x2cRate, axcBalance)
				}
			},
		},
		"non-AXC UTXO": {
			setup: func(t *testing.T, vm *VM, sharedMemory *atomic.Memory) *Tx {
				txID := ids.GenerateTestID()
				utxo, err := addUTXO(sharedMemory, vm.ctx, txID, 0, assetID, 1, testShortIDAddrs[0])
				if err != nil {
					t.Fatal(err)
				}

				tx := &Tx{UnsignedAtomicTx: &UnsignedImportTx{
					NetworkID:    vm.ctx.NetworkID,
					BlockchainID: vm.ctx.ChainID,
					SourceChain:  vm.ctx.SwapChainID,
					ImportedInputs: []*axc.TransferableInput{{
						UTXOID: utxo.UTXOID,
						Asset:  axc.Asset{ID: assetID},
						In: &secp256k1fx.TransferInput{
							Amt:   1,
							Input: secp256k1fx.Input{SigIndices: []uint32{0}},
						},
					}},
					Outs: []EVMOutput{{
						Address: testEthAddrs[0],
						Amount:  1,
						AssetID: assetID,
					}},
				}}
				if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
					t.Fatal(err)
				}
				return tx
			},
			checkState: func(t *testing.T, vm *VM) {
				lastAcceptedBlock := vm.LastAcceptedBlockInternal().(*Block)

				sdb, err := vm.chain.BlockState(lastAcceptedBlock.ethBlock)
				if err != nil {
					t.Fatal(err)
				}

				assetBalance := sdb.GetBalanceMultiCoin(testEthAddrs[0], common.Hash(assetID))
				if assetBalance.Cmp(common.Big1) != 0 {
					t.Fatalf("Expected asset balance to be %d, found balance: %d", common.Big1, assetBalance)
				}
				axcBalance := sdb.GetBalance(testEthAddrs[0])
				if axcBalance.Cmp(common.Big0) != 0 {
					t.Fatalf("Expected AXC balance to be 0, found balance: %d", axcBalance)
				}
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executeTxTest(t, test)
		})
	}
}
