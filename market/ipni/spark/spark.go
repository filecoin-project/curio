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
	if build.BuildType == build.BuildMainnet {
		return "", nil
	}

	if build.BuildType == build.BuildCalibnet {
		return "0xE08bBc65aF1f1a40BD3cbaD290a23925F83b8BBB", nil
	}

	if build.BuildType == build.Build2k || build.BuildType == build.BuildDebug {
		return "", errors.New("manual contract is required for debug build")
	}

	return "", errors.New("unknown build type")
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
    },
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
    },
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
          "components": [
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
          "internalType": "struct MinerPeerIDMapping.PeerData",
          "name": "",
          "type": "tuple"
        }
      ],
      "stateMutability": "view",
      "type": "function"
    },
]`
