package ipcclient

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/iotaledger/giota"
	"github.com/muxxer/diverdriver/common"
	"github.com/muxxer/diverdriver/common/ipccommon"
	"github.com/sigurn/crc8"
)

var (
	IpcClient = &common.ClientAPI{
		PowFuncDefinition:    PowFunc,
		GetPowInfoDefinition: GetPowInfo,
	}
)

func getServerVersion(p *common.DiverClient) (serverVersion string, Error error) {
	serverVersionBytes, err := sendIpcFrameV1ToServer(p, ipccommon.IpcCmdGetServerVersion, nil)
	return string(serverVersionBytes), err
}

func getPowType(p *common.DiverClient) (powType string, Error error) {
	powTypeBytes, err := sendIpcFrameV1ToServer(p, ipccommon.IpcCmdGetPowType, nil)
	return string(powTypeBytes), err
}

func getPowVersion(p *common.DiverClient) (powVersion string, Error error) {
	powVersionBytes, err := sendIpcFrameV1ToServer(p, ipccommon.IpcCmdGetPowVersion, nil)
	return string(powVersionBytes), err
}

// GetPowInfo returns information about the diverDriver version, POW hardware type, and POW hardware version
func GetPowInfo(p *common.DiverClient) (ServerVersion string, PowType string, PowVersion string, Error error) {
	serverVersion, err := getServerVersion(p)
	if err != nil {
		return "", "", "", err
	}

	powType, err := getPowType(p)
	if err != nil {
		return "", "", "", err
	}

	powVersion, err := getPowVersion(p)
	if err != nil {
		return "", "", "", err
	}

	return serverVersion, powType, powVersion, nil
}

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
	data := []byte{byte(minWeightMagnitude)}
	data = append(data, []byte(string(trytes))...)

	response, err := sendIpcFrameV1ToServer(p, ipccommon.IpcCmdPowFunc, data)
	responseString := string(response)
	if err != nil {
		return "", err
	}

	return giota.ToTrytes(responseString)
}

// sendToServer sends an IpcMessage struct to the diverDriver
// It returns the response bytes or an error
func sendToServer(p *common.DiverClient, requestMsg *ipccommon.IpcMessage) (response []byte, Error error) {
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
func sendIpcFrameV1ToServer(p *common.DiverClient, command byte, data []byte) (response []byte, Error error) {
	p.RequestIdLock.Lock()
	p.RequestId++
	reqID := p.RequestId
	p.RequestIdLock.Unlock()

	requestMsg, err := ipccommon.NewIpcMessageV1(reqID, command, data)
	if err != nil {
		return nil, err
	}

	resp, err := sendToServer(p, requestMsg)
	if err != nil {
		return nil, err
	}

	frame, err := ipccommon.BytesToIpcFrameV1(resp)
	if err != nil {
		return nil, err
	}

	if frame.ReqID != reqID {
		return nil, fmt.Errorf("Wrong ReqID! ReqID: %X, Expected: %X", frame.ReqID, reqID)
	}

	switch frame.Command {

	case ipccommon.IpcCmdResponse:
		return frame.Data, nil

	case ipccommon.IpcCmdError:
		return nil, fmt.Errorf(string(frame.Data))

	default:
		//
		// IpcCmdNotification, IpcCmdGetServerVersion, IpcCmdGetPowType, IpcCmdGetPowVersion, IpcCmdPowFunc
		return nil, fmt.Errorf("Unknown command! Cmd: %X", frame.Command)
	}
}

func receive(c net.Conn, timeoutMs int) (response []byte, Error error) {
	frameState := ipccommon.FrameStateSearchEnq
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

				case ipccommon.FrameStateSearchEnq:
					if buf[bufferIdx] == 0x05 {
						// Init variables for new message
						frameLength = -1
						frameData = nil
						frameState = ipccommon.FrameStateSearchVersion
					}

				case ipccommon.FrameStateSearchVersion:
					if buf[bufferIdx] == 0x01 {
						frameState = ipccommon.FrameStateSearchLength
					} else {
						frameState = ipccommon.FrameStateSearchEnq
					}

				case ipccommon.FrameStateSearchLength:
					if frameLength == -1 {
						// Receive first byte
						frameLength = int(buf[bufferIdx]) << 8
					} else {
						// Receive second byte and go on
						frameLength |= int(buf[bufferIdx])
						frameState = ipccommon.FrameStateSearchData
					}

				case ipccommon.FrameStateSearchData:
					missingByteCount := frameLength - len(frameData)
					if (bufLength - bufferIdx) >= missingByteCount {
						// Frame completely received
						frameData = append(frameData, buf[bufferIdx:(bufferIdx+missingByteCount)]...)
						bufferIdx += missingByteCount - 1
						frameState = ipccommon.FrameStateSearchCRC
					} else {
						// Frame not completed in this read => Copy the remaining bytes
						frameData = append(frameData, buf[bufferIdx:bufLength]...)
						bufferIdx = bufLength
					}

				case ipccommon.FrameStateSearchCRC:
					crc := crc8.Checksum(frameData, ipccommon.Crc8Table)
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
