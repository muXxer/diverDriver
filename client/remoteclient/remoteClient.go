package remoteclient

import (
	"fmt"

	"github.com/iotaledger/giota"
	"github.com/muxxer/diverdriver/common"
	remotePoWClient "gitlab.com/brunoamancio/remotePoW/client"
)

var (
	RemoteClient = &common.ClientAPI{
		PowFuncDefinition:    PowFunc,
		GetPowInfoDefinition: GetPowInfo,
	}
)

// PowFunc does the POW
func PowFunc(p *common.DiverClient, trytes giota.Trytes, minWeightMagnitude int) (result giota.Trytes, Error error) {
	if (minWeightMagnitude < 0) || (minWeightMagnitude > 243) {
		return "", fmt.Errorf("minWeightMagnitude out of range [0-243]: %v", minWeightMagnitude)
	}

	result, err := doPow(p, trytes, minWeightMagnitude)
	if err != nil {
		return "", err
	}

	return result, err
}

func doPow(p *common.DiverClient, trytes giota.Trytes, minWeightMagnitude int) (giota.Trytes, error) {
	trytesWithPowString, err := remotePoWClient.DoRemotePoW(p.DiverDriverPath, string(trytes), minWeightMagnitude)
	if err != nil {
		return "", err
	}
	// 2646 is the offset of the nonce in a transaction
	nounce := trytesWithPowString[2646:]
	return giota.Trytes(nounce), err
}

func GetPowInfo(p *common.DiverClient) (ServerVersion string, PowType string, PowVersion string, Error error) {
	serverVersionString, powTypeString, powVersionString, err := remotePoWClient.GetPoWInfo(p.DiverDriverPath)
	return serverVersionString, powTypeString, powVersionString, err
}

// Not used yet, but its available for individual requests
func getServerVersion(p *common.DiverClient) (serverVersion string, Error error) {
	serverVersionString, err := remotePoWClient.GetServerVersion(p.DiverDriverPath)
	return serverVersionString, err
}

// Not used yet, but its available for individual requests
func getPowType(p *common.DiverClient) (powType string, Error error) {
	powTypeString, err := remotePoWClient.GetPoWType(p.DiverDriverPath)
	return powTypeString, err
}

// Not used yet, but its available for individual requests
func getPowVersion(p *common.DiverClient) (powVersion string, Error error) {
	powVersionString, err := remotePoWClient.GetPoWVersion(p.DiverDriverPath)
	return powVersionString, err
}
