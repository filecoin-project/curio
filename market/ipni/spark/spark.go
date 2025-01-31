package spark

import (
	"errors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/build"
)

type SparkMessage struct {
	Miner abi.ActorID `json:"miner"`
	Peer  string      `json:"peer"`
}

func GetContractAddress() (string, error) {

	if build.BuildType != build.BuildMainnet {
		return "", errors.New("not supported on this network")
	}

	return "0x792A226A73CAa73DD95b88b37DA29717b950C7D3", nil
}

const AddPeerAbi = `[
    {
      "inputs": [
        {
          "internalType": "uint64",
          "name": "minerID",
          "type": "uint64"
        },
        {
          "internalType": "string",
          "name": "newPeerID",
          "type": "string"
        },
        {
          "internalType": "bytes",
          "name": "signedMessage",
          "type": "bytes"
        }
      ],
      "name": "addPeerData",
      "outputs": [],
      "stateMutability": "nonpayable",
      "type": "function"
    }
]`

const UpdatePeerAbi = `[
	{
      "inputs": [
        {
          "internalType": "uint64",
          "name": "minerID",
          "type": "uint64"
        },
        {
          "internalType": "string",
          "name": "newPeerID",
          "type": "string"
        },
        {
          "internalType": "bytes",
          "name": "signedMessage",
          "type": "bytes"
        }
      ],
      "name": "updatePeerData",
      "outputs": [],
      "stateMutability": "nonpayable",
      "type": "function"
    }
]`

const DeletePeerAbi = `[
	{
      "inputs": [
        {
          "internalType": "uint64",
          "name": "minerID",
          "type": "uint64"
        }
      ],
      "name": "deletePeerData",
      "outputs": [],
      "stateMutability": "nonpayable",
      "type": "function"
    }
]`

const GetPeerAbi = `[
	{
      "inputs": [
        {
          "internalType": "uint64",
          "name": "minerID",
          "type": "uint64"
        }
      ],
      "name": "getPeerData",
      "outputs": [
        {
          "internalType": "string",
          "name": "peerID",
          "type": "string"
        },
        {
          "internalType": "bytes",
          "name": "signedMessage",
          "type": "bytes"
        }
      ],
      "stateMutability": "view",
      "type": "function"
    }
]`
