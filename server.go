package diverdriver

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/iotaledger/giota"
	"github.com/lunixbochs/struc"
	"github.com/sigurn/crc8"
	"github.com/spf13/viper"

	"github.com/muxxer/diverdriver/logs"
)

const (
	// Different states of the receivement of the frame via interprocess communication
	FrameStateSearchEnq     byte = 1 // FrameStateSearchEnq: Search the Start byte of the frame
	FrameStateSearchVersion byte = 2 // Search the Version byte of the frame
	FrameStateSearchLength  byte = 3 // Search the length information of the frame
	FrameStateSearchData    byte = 4 // Search all the data embedded in the frame
	FrameStateSearchCRC     byte = 5 // Search the CRC checksum of the embedded data

	IpcCmdNotification     = 0x01 // S => C: Text messages to the client
	IpcCmdResponse         = 0x02 // S => C: Response to a IPC_CMD
	IpcCmdError            = 0x03 // S => C: Exceptions that should be raised in the client
	IpcCmdGetServerVersion = 0x04 // C => S: Get the version of this application
	IpcCmdGetPowType       = 0x05 // C => S: Get the name of the used POW implementation (e.g. PiDiver)
	IpcCmdGetPowVersion    = 0x06 // C => S: Get the version of the used POW implementation (e.g. PiDiver FPGA Core Version)
	IpcCmdPowFunc          = 0x07 // C => S: Do POW

	diverDriverVersion = "0.2.0"
)

var crc8Table = crc8.MakeTable(crc8.CRC8_MAXIM)
var powMutex = &sync.Mutex{}
var powFuncPtr giota.PowFunc

/*
	Interprocess communication protocol
	===================================

	[0] START_BYTE | [1] FRAME_VERSION | [2..3] FRAME_LENGTH | [4..4+FRAME_LENGTH] FRAME_DATA | [4+FRAME_LENGTH] CRC8

	START_BYTE:
		Start of the IPC frame
		ENQ Byte (0x05) - Enquiry

	FRAME_VERSION:
		Version of the IPC frame, for future extensions of the protocol

	FRAME_LENGTH:
		Size of the FRAME_DATA

	FRAME_DATA:
		----- FRAME_VERSION==0x01 -----

		[4] REQ_ID | [5] IPC_CMD | [6..7] DATA_LENGTH | [8..8+DATA_LENGTH] DATA

		REQ_ID:
			ID of the message, set by the client.
			Server will respond to the client with the same ID.
			This way the client knows which response is assigned to which request.

		IPC_CMD:
			IpcCmdNotification     = 0x01 // S => C: Text messages to the client
			IpcCmdResponse         = 0x02 // S => C: Response to a IPC_CMD
			IpcCmdError            = 0x03 // S => C: Exceptions that should be raised in the client
			IpcCmdGetServerVersion = 0x04 // C => S: Get the version of this application
			IpcCmdGetPowType       = 0x05 // C => S: Get the name of the used POW implementation (e.g. PiDiver)
			IpcCmdGetPowVersion    = 0x06 // C => S: Get the version of the used POW implementation (e.g. PiDiver FPGA Core Version)
			IpcCmdPowFunc          = 0x07 // C => S: Do POW

		DATA_LENGTH:
			Size of the DATA

		DATA:
			Data with variable length

			----- IPC_CMD==IpcCmdNotification -----
			[8..8+DATA_LENGTH]	String	Notification

			----- IPC_CMD==IpcCmdResponse -----
			[8..8+DATA_LENGTH] ReponseData

			----- IPC_CMD==IpcCmdError -----
			[8..8+DATA_LENGTH] ExceptionMessage

			----- IPC_CMD==IpcCmdGetServerVersion -----
			[8..8+DATA_LENGTH] 	String	ServerVersion

			----- IPC_CMD==IpcCmdGetPowType -----
			[8..8+DATA_LENGTH] 	String	PowType

			----- IPC_CMD==IpcCmdGetPowVersion -----
			[8..8+DATA_LENGTH] 	String	PowVersion

			----- IPC_CMD==IpcCmdPowFunc ----
			[8..8+DATA_LENGTH] 	Trytes POW result

	CRC8:
		Checksum of the whole FRAME_DATA

*/

// IpcMessage is the container of an IPC frame with additional communication control data
type IpcMessage struct {
	StartByte    byte   `struc:"byte"`
	FrameVersion byte   `struc:"byte"`
	FrameLength  int    `struc:"uint16,sizeof=FrameData"`
	FrameData    []byte `struc:"[]byte"`
	CRC8         byte   `struc:"byte"`
}

// IpcFrameV1 contains the information of the IPC communication
type IpcFrameV1 struct {
	ReqID      byte   `struc:"byte"`
	Command    byte   `struc:"byte"`
	DataLength int    `struc:"uint16,sizeof=Data"`
	Data       []byte `struc:"[]byte"`
}

// ToBytes converts an IpcMessage to a byte slice
func (m *IpcMessage) ToBytes() ([]byte, error) {
	var buf bytes.Buffer
	err := struc.Pack(&buf, m)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ToBytes converts an IpcFrameV1 to a byte slice
func (f *IpcFrameV1) ToBytes() ([]byte, error) {
	var buf bytes.Buffer
	err := struc.Pack(&buf, f)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// BytesToIpcMessage converts a byte slice to an IpcMessage
func BytesToIpcMessage(data []byte) (*IpcMessage, error) {
	buf := bytes.NewBuffer(data)

	msg := new(IpcMessage)
	err := struc.Unpack(buf, &msg)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

// BytesToIpcFrameV1 converts a byte slice to an IpcFrameV1
func BytesToIpcFrameV1(data []byte) (*IpcFrameV1, error) {
	buf := bytes.NewBuffer(data)

	frame := new(IpcFrameV1)
	err := struc.Unpack(buf, &frame)
	if err != nil {
		return nil, err
	}

	return frame, nil
}

// NewIpcMessageV1 creates a new IpcFrameV1 embedded in an IpcMessage
func NewIpcMessageV1(requestID byte, command byte, data []byte) (*IpcMessage, error) {
	frameLength := len(data)
	if frameLength > 0xFFFF {
		return nil, errors.New("Message is too big")
	}

	frame := &IpcFrameV1{ReqID: requestID, Command: command, DataLength: len(data), Data: data}
	frameBytes, err := frame.ToBytes()
	if err != nil {
		return nil, err
	}

	crc8 := crc8.Checksum(frameBytes, crc8Table)
	message := &IpcMessage{StartByte: 0x05, FrameVersion: 0x01, FrameLength: frameLength, FrameData: frameBytes, CRC8: crc8}

	return message, nil
}

// sendToClient sends an IpcMessage to a client
func sendToClient(c net.Conn, responseMsg *IpcMessage) (err error) {
	response, err := responseMsg.ToBytes()
	if err != nil {
		return err
	}

	_, err = c.Write(response)

	return err
}

// SetPowFunc sets the function pointer for POW
func SetPowFunc(f giota.PowFunc) {
	powFuncPtr = f
}

// powFunc calls the hardware POW secured by a Mutex
func powFunc(trytes giota.Trytes, mwm int) (giota.Trytes, error) {
	powMutex.Lock()
	defer powMutex.Unlock()

	if powFuncPtr == nil {
		return "", errors.New("powFunc not initialized")
	}

	logs.Log.Debugf("Starting PoW! Weight: %d", mwm)
	ts := time.Now()
	result, err := powFuncPtr(trytes, mwm)
	logs.Log.Debugf("Finished PoW! Time: %d [ms]", (int64(time.Since(ts) / time.Millisecond)))

	return result, err
}

// HandleClientConnection handles the communication to the client until the socket is closed
func HandleClientConnection(c net.Conn, config *viper.Viper, powType string, powVersion string) {
	frameState := FrameStateSearchEnq
	frameLength := 0
	var frameData []byte
	defer c.Close()

	for {
		buf := make([]byte, 3072) // ((8019 is the TransactionTrinarySize) / 3) + Overhead) => 3072
		bufLength, err := c.Read(buf)
		if err != nil {
			break
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
					frame, err := BytesToIpcFrameV1(frameData)
					if err != nil {
						logs.Log.Debug(err.Error())
						responseMsg, _ := NewIpcMessageV1(0, IpcCmdError, []byte(err.Error()))
						sendToClient(c, responseMsg)
						frameState = FrameStateSearchEnq
						break
					}

					crc := crc8.Checksum(frameData, crc8Table)
					if buf[bufferIdx] != crc {
						logs.Log.Debugf("Wrong Checksum! CRC: %X, Expected: %X", crc, buf[bufferIdx])
						responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdError, []byte(fmt.Sprintf("Wrong Checksum! CRC: %X, Expected: %X", crc, buf[bufferIdx])))
						sendToClient(c, responseMsg)
						frameState = FrameStateSearchEnq
						break
					}

					switch frame.Command {

					case IpcCmdGetServerVersion:
						logs.Log.Debug("Received Command GetServerVersion")
						responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdResponse, []byte(diverDriverVersion))
						sendToClient(c, responseMsg)

					case IpcCmdGetPowType:
						logs.Log.Debug("Received Command GetPowType")
						responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdResponse, []byte(powType))
						sendToClient(c, responseMsg)

					case IpcCmdGetPowVersion:
						logs.Log.Debug("Received Command GetPowVersion")
						responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdResponse, []byte(powVersion))
						sendToClient(c, responseMsg)

					case IpcCmdPowFunc:
						logs.Log.Debug("Received Command PowFunc")
						mwm := int(frame.Data[0])

						if mwm > config.GetInt("pow.maxMinWeightMagnitude") {
							logs.Log.Debugf("MinWeightMagnitude too high. MWM: %v Allowed: %v", mwm, config.GetInt("pow.maxMinWeightMagnitude"))
							responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdError, []byte(fmt.Sprintf("MinWeightMagnitude too high. MWM: %v Allowed: %v", mwm, config.GetInt("pow.maxMinWeightMagnitude"))))
							sendToClient(c, responseMsg)
							frameState = FrameStateSearchEnq
							break
						}

						trytes, err := giota.ToTrytes(string(frame.Data[1:]))
						if err != nil {
							logs.Log.Debug(err.Error())
							responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdError, []byte(err.Error()))
							sendToClient(c, responseMsg)
							frameState = FrameStateSearchEnq
							break
						}

						result, err := powFunc(trytes, mwm)
						if err != nil {
							logs.Log.Debug(err.Error())
							responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdError, []byte(err.Error()))
							sendToClient(c, responseMsg)
							frameState = FrameStateSearchEnq
							break
						} else {
							responseMsg, err := NewIpcMessageV1(frame.ReqID, IpcCmdResponse, []byte(result))
							if err != nil {
								frameState = FrameStateSearchEnq
								break
							}
							sendToClient(c, responseMsg)
						}

					default:
						// IpcCmdNotification, IpcCmdResponse, IpcCmdError
						logs.Log.Debugf("Unknown command! Cmd: %X", frame.Command)
						responseMsg, _ := NewIpcMessageV1(frame.ReqID, IpcCmdError, []byte(fmt.Sprintf("Unknown command! Cmd: %X", frame.Command)))
						sendToClient(c, responseMsg)
					}

					// Search for the next message
					frameState = FrameStateSearchEnq
				}
			} else {
				// Received Buffer completely handled, break the loop to receive the next message
				break
			}
		}
	}
}
