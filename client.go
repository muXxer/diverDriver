package diverdriver

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/iotaledger/giota"
	"github.com/sigurn/crc8"
	remotePoWClient "gitlab.com/brunoamancio/remotePoW/client"
)

// DiverClient is the client that connects to the diverDriver
type DiverClient struct {
	DiverDriverPath string // Path to the diverDriver Unix socket
	WriteTimeOutMs  int64  // Timeout in ms to write to the Unix socket
	ReadTimeOutMs   int    // Timeout in ms to read the Unix socket
	RequestId       byte
	RequestIdLock   sync.Mutex
}

func receive(c net.Conn, timeoutMs int) (response []byte, Error error) {
	frameState := FrameStateSearchEnq
	frameLength := 0
	var frameData []byte

	ts := time.Now()
	td := time.Duration(timeoutMs) * time.Millisecond

	for {
		if time.Since(ts) > td {
			return nil, errors.New("Receive timeout")
		}

		buf := make([]byte, 3072) // ((8019 is the TransactionTrinarySize) / 3) + Overhead) => 3072
		bufLength, err := c.Read(buf)
		if err != nil {
			continue
		}

		bufferIdx := -1
		for {
			bufferIdx++

			if bufLength > bufferIdx {
				switch frameState {

				case FrameStateSearchEnq:
					if buf[bufferIdx] == 0x05 {
						// Init variables for new message
						frameLength = -1
						frameData = nil
						frameState = FrameStateSearchVersion
					}

				case FrameStateSearchVersion:
					if buf[bufferIdx] == 0x01 {
						frameState = FrameStateSearchLength
					} else {
						frameState = FrameStateSearchEnq
					}

				case FrameStateSearchLength:
					if frameLength == -1 {
						// Receive first byte
						frameLength = int(buf[bufferIdx]) << 8
					} else {
						// Receive second byte and go on
						frameLength |= int(buf[bufferIdx])
						frameState = FrameStateSearchData
					}

				case FrameStateSearchData:
					missingByteCount := frameLength - len(frameData)
					if (bufLength - bufferIdx) >= missingByteCount {
						// Frame completely received
						frameData = append(frameData, buf[bufferIdx:(bufferIdx+missingByteCount)]...)
						bufferIdx += missingByteCount - 1
						frameState = FrameStateSearchCRC
					} else {
						// Frame not completed in this read => Copy the remaining bytes
						frameData = append(frameData, buf[bufferIdx:bufLength]...)
						bufferIdx = bufLength
					}

				case FrameStateSearchCRC:
					crc := crc8.Checksum(frameData, crc8Table)
					if buf[bufferIdx] != crc {
						return nil, fmt.Errorf("Wrong Checksum! CRC: %X, Expected: %X", crc, buf[bufferIdx])
					}

					return frameData, nil

				}
			} else {
				// Received Buffer completely handled, break the loop to receive the next message
				break
			}
		}
	}
}

// sendToServer sends an IpcMessage struct to the diverDriver
// It returns the response bytes or an error
func (p *DiverClient) sendToServer(requestMsg *IpcMessage) (response []byte, Error error) {
	request, err := requestMsg.ToBytes()
	if err != nil {
		return nil, err
	}

	c, err := net.Dial("unix", p.DiverDriverPath)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	if p.WriteTimeOutMs != 0 {
		err = c.SetWriteDeadline(time.Now().Add(time.Millisecond * time.Duration(p.WriteTimeOutMs)))
		if err != nil {
			return nil, err
		}
	}

	if p.ReadTimeOutMs != 0 {
		err = c.SetReadDeadline(time.Now().Add(time.Millisecond * time.Duration(p.ReadTimeOutMs)))
		if err != nil {
			return nil, err
		}
	}

	_, err = c.Write(request)
	if err != nil {
		return nil, err
	}

	response, err = receive(c, p.ReadTimeOutMs)
	return response, err
}

// sendIpcFrameV1ToServer creates an IpcFrameV1 and calls sendToServer
// The answer of the server is evaluated and returned to the caller
func (p *DiverClient) sendIpcFrameV1ToServer(command byte, data []byte) (response []byte, Error error) {
	p.RequestIdLock.Lock()
	p.RequestId++
	reqID := p.RequestId
	p.RequestIdLock.Unlock()

	requestMsg, err := NewIpcMessageV1(reqID, command, data)
	if err != nil {
		return nil, err
	}

	resp, err := p.sendToServer(requestMsg)
	if err != nil {
		return nil, err
	}

	frame, err := BytesToIpcFrameV1(resp)
	if err != nil {
		return nil, err
	}

	if frame.ReqID != reqID {
		return nil, fmt.Errorf("Wrong ReqID! ReqID: %X, Expected: %X", frame.ReqID, reqID)
	}

	switch frame.Command {

	case IpcCmdResponse:
		return frame.Data, nil

	case IpcCmdError:
		return nil, fmt.Errorf(string(frame.Data))

	default:
		//
		// IpcCmdNotification, IpcCmdGetServerVersion, IpcCmdGetPowType, IpcCmdGetPowVersion, IpcCmdPowFunc
		return nil, fmt.Errorf("Unknown command! Cmd: %X", frame.Command)
	}
}

func (p *DiverClient) getServerVersion() (serverVersion string, Error error) {
	isURL := isValidRemoteURL(p.DiverDriverPath)
	if isURL {
		serverVersionString, err := remotePoWClient.GetServerVersion(p.DiverDriverPath)
		return serverVersionString, err
	}

	serverVersionBytes, err := p.sendIpcFrameV1ToServer(IpcCmdGetServerVersion, nil)
	return string(serverVersionBytes), err
}

func (p *DiverClient) getPowType() (powType string, Error error) {
	isURL := isValidRemoteURL(p.DiverDriverPath)
	if isURL {
		powTypeString, err := remotePoWClient.GetPoWType(p.DiverDriverPath)
		return powTypeString, err
	}

	powTypeBytes, err := p.sendIpcFrameV1ToServer(IpcCmdGetPowType, nil)
	return string(powTypeBytes), err
}

func (p *DiverClient) getPowVersion() (powVersion string, Error error) {
	isURL := isValidRemoteURL(p.DiverDriverPath)
	if isURL {
		powVersionString, err := remotePoWClient.GetPoWVersion(p.DiverDriverPath)
		return powVersionString, err
	}

	powVersionBytes, err := p.sendIpcFrameV1ToServer(IpcCmdGetPowVersion, nil)
	return string(powVersionBytes), err
}

func (p *DiverClient) getRemotePowInfo() (serverVersion string, powType string, powVersion string, Error error) {
	isURL := isValidRemoteURL(p.DiverDriverPath)
	if isURL {
		serverVersionString, powTypeString, powVersionString, err := remotePoWClient.GetPoWInfo(p.DiverDriverPath)
		return serverVersionString, powTypeString, powVersionString, err
	}

	return "", "", "", errors.New("Invalid URL")
}

// GetPowInfo returns information about the diverDriver version, POW hardware type, and POW hardware version
func (p *DiverClient) GetPowInfo() (ServerVersion string, PowType string, PowVersion string, Error error) {
	isURL := isValidRemoteURL(p.DiverDriverPath)
	if isURL {
		return p.getRemotePowInfo()
	}

	serverVersion, err := p.getServerVersion()
	if err != nil {
		return "", "", "", err
	}

	powType, err := p.getPowType()
	if err != nil {
		return "", "", "", err
	}

	powVersion, err := p.getPowVersion()
	if err != nil {
		return "", "", "", err
	}

	return serverVersion, powType, powVersion, nil
}

// PowFunc does the POW
func (p *DiverClient) PowFunc(trytes giota.Trytes, minWeightMagnitude int) (result giota.Trytes, Error error) {
	if (minWeightMagnitude < 0) || (minWeightMagnitude > 243) {
		return "", fmt.Errorf("minWeightMagnitude out of range [0-243]: %v", minWeightMagnitude)
	}

	result, err := doPow(p, trytes, minWeightMagnitude)
	if err != nil {
		return "", err
	}

	return result, err
}

func doPow(p *DiverClient, trytes giota.Trytes, minWeightMagnitude int) (giota.Trytes, error) {
	isURL := isValidRemoteURL(p.DiverDriverPath)
	if isURL {
		trytesWithPowString, err := remotePoWClient.DoRemotePoW(p.DiverDriverPath, string(trytes), minWeightMagnitude)
		if err != nil {
			return "", err
		}
		// 2646 is the nounce offset in a transaction
		nounce := trytesWithPowString[2646:]
		return giota.Trytes(nounce), err
	}

	data := []byte{byte(minWeightMagnitude)}
	data = append(data, []byte(string(trytes))...)

	response, err := p.sendIpcFrameV1ToServer(IpcCmdPowFunc, data)
	responseString := string(response)
	if err != nil {
		return "", err
	}

	return giota.ToTrytes(responseString)
}

func isValidRemoteURL(toTest string) bool {
	uri, err := url.ParseRequestURI(toTest)
	hostname := uri.Hostname()
	if err != nil || hostname == "" {
		return false
	}
	return true
}
