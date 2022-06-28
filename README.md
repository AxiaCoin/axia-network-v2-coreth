# Coreth and the AX-Chain

[Axia](https://docs.axc.network/learn/platform-overview) is a network composed of multiple blockchains.
Each blockchain is an instance of a Virtual Machine (VM), much like an object in an object-oriented language is an instance of a class.
That is, the VM defines the behavior of the blockchain.
Coreth (from core Ethereum) is the [Virtual Machine (VM)](https://docs.axc.network/learn/platform-overview#virtual-machines) that defines the Contract Chain (AX-Chain).
This chain implements the Ethereum Virtual Machine and supports Solidity smart contracts as well as most other Ethereum client functionality.

## Building

Coreth is a dependency of Axia which is used to implement the EVM based Virtual Machine for the Axia AX-Chain. In order to run with a local version of Coreth, users must update their Coreth dependency within Axia to point to their local Coreth directory. If Coreth and Axia are at the standard location within your GOPATH, this will look like the following:

```bash
cd $GOPATH/src/github.com/axiacoin/axia-network-v2
go mod edit -replace github.com/axiacoin/axia-network-v2-coreth=../coreth
```

Note: the AX-Chain originally ran in a separate process from the main Axia process and communicated with it over a local gRPC connection. When this was the case, Axia's build script would download Coreth, compile it, and place the binary into the `axia/build/plugins` directory.

## API

The AX-Chain supports the following API namespaces:

- `eth`
- `personal`
- `txpool`
- `debug`

Only the `eth` namespace is enabled by default. 
To enable the other namespaces see the instructions for passing in the `coreth-config` parameter to Axia: https://docs.axc.network/build/references/command-line-interface#plugins.
Full documentation for the AX-Chain's API can be found [here.](https://docs.axc.network/build/axia-apis/ax-chain)

## Compatibility

The AX-Chain is compatible with almost all Ethereum tooling, including [Remix,](https://docs.axc.network/build/tutorials/smart-contracts/deploy-a-smart-contract-on-axia-using-remix-and-metamask) [Metamask](https://docs.axc.network/build/tutorials/smart-contracts/deploy-a-smart-contract-on-axia-using-remix-and-metamask) and [Truffle.](https://docs.axc.network/build/tutorials/smart-contracts/using-truffle-with-the-axia-ax-chain)

## Differences Between Axia AX-Chain and Ethereum

### Atomic Transactions

As a network composed of multiple blockchains, Axia uses *atomic transactions* to move assets between chains. Coreth modifies the Ethereum block format by adding an *ExtraData* field, which contains the atomic transactions.

### Axia Native Tokens (ANTs)

The AX-Chain supports Axia Native Tokens, which are created on the Swap-Chain using precompiled contracts. These precompiled contracts *nativeAssetCall* and *nativeAssetBalance* support the same interface for ANTs as *CALL* and *BALANCE* do for AXC with the added parameter of *assetID* to specify the asset.

For the full documentation of precompiles for interacting with ANTs and using them in ARC-20s, see [here](https://docs.axc.network/build/references/coreth-arc20s).

### Block Timing

Blocks are produced asynchronously in Snowman Consensus, so the timing assumptions that apply to Ethereum do not apply to Coreth. To support block production in an async environment, a block is permitted to have the same timestamp as its parent. Since there is no general assumption that a block will be produced every 10 seconds, smart contracts built on Axia should use the block timestamp instead of the block number for their timing assumptions.

A block with a timestamp more than 10 seconds in the future will not be considered valid. However, a block with a timestamp more than 10 seconds in the past will still be considered valid as long as its timestamp is greater than or equal to the timestamp of its parent block.

## Difficulty and Random OpCode

Snowman consensus does not use difficulty in any way, so the difficulty of every block is required to be set to 1. This means that the DIFFICULTY opcode should not be used as a source of randomness.

Additionally, with the change from the DIFFICULTY OpCode to the RANDOM OpCode (RANDOM replaces DIFFICULTY directly), there is no planned change to provide a stronger source of randomness. The RANDOM OpCode relies on the Eth2.0 Randomness Beacon, which has no direct parallel within the context of either Coreth or Snowman consensus. Therefore, instead of providing a weaker source of randomness that may be manipulated, the RANDOM OpCode will not be supported. Instead, it will continue the behavior of the DIFFICULTY OpCode of returning the block's difficulty, such that it will always return 1.
