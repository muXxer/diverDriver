package ipccommon

import (
	"bytes"
	"errors"

	"github.com/lunixbochs/struc"
	"github.com/sigurn/crc8"
)

const (
	IpcCmdNotification     = 0x01 // S => C: Text messages to the client
	IpcCmdResponse         = 0x02 // S => C: Response to a IPC_CMD
	IpcCmdError            = 0x03 // S => C: Exceptions that should be raised in the client
	IpcCmdGetServerVersion = 0x04 // C => S: Get the version of this application
	IpcCmdGetPowType       = 0x05 // C => S: Get the name of the used POW implementation (e.g. PiDiver)
	IpcCmdGetPowVersion    = 0x06 // C => S: Get the version of the used POW implementation (e.g. PiDiver FPGA Core Version)
	IpcCmdPowFunc          = 0x07 // C => S: Do POW

	// Different states of the receivement of the frame via interprocess communication
	FrameStateSearchEnq     byte = 1 // FrameStateSearchEnq: Search the Start byte of the frame
	FrameStateSearchVersion byte = 2 // Search the Version byte of the frame
	FrameStateSearchLength  byte = 3 // Search the length information of the frame
	FrameStateSearchData    byte = 4 // Search all the data embedded in the frame
	FrameStateSearchCRC     byte = 5 // Search the CRC checksum of the embedded data
)

var Crc8Table = crc8.MakeTable(crc8.CRC8_MAXIM)

// IpcFrameV1 contains the information of the IPC communication
type IpcFrameV1 struct {
	ReqID      byte   `struc:"byte"`
	Command    byte   `struc:"byte"`
	DataLength int    `struc:"uint16,sizeof=Data"`
	Data       []byte `struc:"[]byte"`
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

	crc8 := crc8.Checksum(frameBytes, Crc8Table)
	message := &IpcMessage{StartByte: 0x05, FrameVersion: 0x01, FrameLength: frameLength, FrameData: frameBytes, CRC8: crc8}

	return message, nil
}

// IpcMessage is the container of an IPC frame with additional communication control data
type IpcMessage struct {
	StartByte    byte   `struc:"byte"`
	FrameVersion byte   `struc:"byte"`
	FrameLength  int    `struc:"uint16,sizeof=FrameData"`
	FrameData    []byte `struc:"[]byte"`
	CRC8         byte   `struc:"byte"`
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
