package client

import (
	"github.com/muxxer/diverdriver/client/ipcclient"
	"github.com/muxxer/diverdriver/client/remoteclient"
	"github.com/muxxer/diverdriver/common"
	"github.com/muxxer/diverdriver/utils"
)

func Initialize(diverDriverPath string, writeTimeOutMs int64, readTimeOutMs int) *common.DiverClient {
	p := &common.DiverClient{DiverDriverPath: diverDriverPath, WriteTimeOutMs: writeTimeOutMs, ReadTimeOutMs: readTimeOutMs}
	if utils.IsValidRemoteURL(p.DiverDriverPath) {
		p.PowClientImplementation = remoteclient.RemoteClient
	} else {
		p.PowClientImplementation = ipcclient.IpcClient
	}
	return p
}
