package ipcserver

import (
	"fmt"
	"net"

	"github.com/iotaledger/giota"
	"github.com/muxxer/diverdriver/common"
	"github.com/muxxer/diverdriver/common/ipccommon"
	"github.com/muxxer/diverdriver/logs"
	"github.com/sigurn/crc8"
	"github.com/spf13/viper"
)

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

// sendToClient sends an IpcMessage to a client
func sendToClient(c net.Conn, responseMsg *ipccommon.IpcMessage) (err error) {
	response, err := responseMsg.ToBytes()
	if err != nil {
		return err
	}

	_, err = c.Write(response)

	return err
}

// HandleClientConnection handles the communication to the client until the socket is closed
func HandleClientConnection(c net.Conn, config *viper.Viper, powType string, powVersion string) {
	frameState := ipccommon.FrameStateSearchEnq
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
					frame, err := ipccommon.BytesToIpcFrameV1(frameData)
					if err != nil {
						logs.Log.Debug(err.Error())
						responseMsg, _ := ipccommon.NewIpcMessageV1(0, ipccommon.IpcCmdError, []byte(err.Error()))
						sendToClient(c, responseMsg)
						frameState = ipccommon.FrameStateSearchEnq
						break
					}

					crc := crc8.Checksum(frameData, ipccommon.Crc8Table)
					if buf[bufferIdx] != crc {
						logs.Log.Debugf("Wrong Checksum! CRC: %X, Expected: %X", crc, buf[bufferIdx])
						responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdError, []byte(fmt.Sprintf("Wrong Checksum! CRC: %X, Expected: %X", crc, buf[bufferIdx])))
						sendToClient(c, responseMsg)
						frameState = ipccommon.FrameStateSearchEnq
						break
					}

					switch frame.Command {

					case ipccommon.IpcCmdGetServerVersion:
						logs.Log.Debug("Received Command GetServerVersion")
						responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdResponse, []byte(common.DiverDriverVersion))
						sendToClient(c, responseMsg)

					case ipccommon.IpcCmdGetPowType:
						logs.Log.Debug("Received Command GetPowType")
						responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdResponse, []byte(powType))
						sendToClient(c, responseMsg)

					case ipccommon.IpcCmdGetPowVersion:
						logs.Log.Debug("Received Command GetPowVersion")
						responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdResponse, []byte(powVersion))
						sendToClient(c, responseMsg)

					case ipccommon.IpcCmdPowFunc:
						logs.Log.Debug("Received Command PowFunc")
						mwm := int(frame.Data[0])

						if mwm > config.GetInt("pow.maxMinWeightMagnitude") {
							logs.Log.Debugf("MinWeightMagnitude too high. MWM: %v Allowed: %v", mwm, config.GetInt("pow.maxMinWeightMagnitude"))
							responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdError, []byte(fmt.Sprintf("MinWeightMagnitude too high. MWM: %v Allowed: %v", mwm, config.GetInt("pow.maxMinWeightMagnitude"))))
							sendToClient(c, responseMsg)
							frameState = ipccommon.FrameStateSearchEnq
							break
						}

						trytes, err := giota.ToTrytes(string(frame.Data[1:]))
						if err != nil {
							logs.Log.Debug(err.Error())
							responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdError, []byte(err.Error()))
							sendToClient(c, responseMsg)
							frameState = ipccommon.FrameStateSearchEnq
							break
						}

						result, err := powFunc(trytes, mwm)
						if err != nil {
							logs.Log.Debug(err.Error())
							responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdError, []byte(err.Error()))
							sendToClient(c, responseMsg)
							frameState = ipccommon.FrameStateSearchEnq
							break
						} else {
							responseMsg, err := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdResponse, []byte(result))
							if err != nil {
								frameState = ipccommon.FrameStateSearchEnq
								break
							}
							sendToClient(c, responseMsg)
						}

					default:
						// IpcCmdNotification, IpcCmdResponse, IpcCmdError
						logs.Log.Debugf("Unknown command! Cmd: %X", frame.Command)
						responseMsg, _ := ipccommon.NewIpcMessageV1(frame.ReqID, ipccommon.IpcCmdError, []byte(fmt.Sprintf("Unknown command! Cmd: %X", frame.Command)))
						sendToClient(c, responseMsg)
					}

					// Search for the next message
					frameState = ipccommon.FrameStateSearchEnq
				}
			} else {
				// Received Buffer completely handled, break the loop to receive the next message
				break
			}
		}
	}
}
