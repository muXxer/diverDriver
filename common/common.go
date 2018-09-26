package common

import (
	"sync"

	"github.com/iotaledger/giota"
)

const (
	DiverDriverVersion = "0.2.0"
)

type PowFuncDefinition func(p *DiverClient, trytes giota.Trytes, minWeightMagnitude int) (result giota.Trytes, Error error)
type GetPowInfoDefinition func(p *DiverClient) (ServerVersion string, PowType string, PowVersion string, Error error)

type ClientAPI struct {
	PowFuncDefinition    PowFuncDefinition
	GetPowInfoDefinition GetPowInfoDefinition
}

// DiverClient is the client that connects to the diverDriver
type DiverClient struct {
	PowClientImplementation *ClientAPI
	DiverDriverPath         string // Path to the diverDriver Unix socket
	WriteTimeOutMs          int64  // Timeout in ms to write to the Unix socket
	ReadTimeOutMs           int    // Timeout in ms to read the Unix socket
	RequestId               byte
	RequestIdLock           sync.Mutex
}

func (p *DiverClient) PowFunc(trytes giota.Trytes, minWeightMagnitude int) (result giota.Trytes, Error error) {
	return p.PowClientImplementation.PowFuncDefinition(p, trytes, minWeightMagnitude)
}

func (p *DiverClient) GetPowFuncDefinition() PowFuncDefinition {
	return p.PowClientImplementation.PowFuncDefinition
}

func (p *DiverClient) GetPowInfo() (ServerVersion string, PowType string, PowVersion string, Error error) {
	return p.PowClientImplementation.GetPowInfoDefinition(p)
}

func (p *DiverClient) GetPowInfoFuncDefinition() PowFuncDefinition {
	return p.PowClientImplementation.PowFuncDefinition
}
