// Package raknet implements the minimal RakNet offline message types needed
// for Minecraft Bedrock server discovery (unconnected ping/pong).
// Most of this code is based on the RakNet protocol offline messages
// https://github.com/Sandertv/go-raknet
package raknet

import (
	"encoding/binary"
	"io"
)

// Unconnected message packet IDs used for server discovery.
const (
	IDUnconnectedPing     byte = 0x01
	IDUnconnectedPingOpen byte = 0x02
	IDUnconnectedPong     byte = 0x1c
)

// unconnectedMessageSequence is a sequence of bytes which is found in every unconnected message sent in
// RakNet.
var unconnectedMessageSequence = [16]byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78}

// UnconnectedPing represents an offline ping packet sent by clients to discover servers.
type UnconnectedPing struct {
	SendTimestamp int64
	Magic         [16]byte
	ClientGUID    int64
}

// UnmarshalBinary decodes an UnconnectedPing from a raw RakNet packet.
func (pk *UnconnectedPing) UnmarshalBinary(data []byte) error {
	if len(data) < 33 {
		return io.ErrUnexpectedEOF
	}
	pk.SendTimestamp = int64(binary.BigEndian.Uint64(data[1:9])) //nolint:gosec // intentional protocol bit-for-bit conversion
	copy(pk.Magic[:], data[9:25])
	pk.ClientGUID = int64(binary.BigEndian.Uint64(data[25:33])) //nolint:gosec // intentional protocol bit-for-bit conversion
	return nil
}

// UnconnectedPong represents an offline pong packet sent by servers responding to pings.
type UnconnectedPong struct {
	SendTimestamp int64
	ServerGUID    int64
	Magic         [16]byte
	Data          []byte
}

// MarshalBinary encodes an UnconnectedPong into a raw RakNet packet.
func (pk *UnconnectedPong) MarshalBinary() []byte {
	b := make([]byte, 35+len(pk.Data))
	b[0] = IDUnconnectedPong
	binary.BigEndian.PutUint64(b[1:9], uint64(pk.SendTimestamp)) //nolint:gosec // intentional protocol bit-for-bit conversion
	binary.BigEndian.PutUint64(b[9:17], uint64(pk.ServerGUID))   //nolint:gosec // intentional protocol bit-for-bit conversion
	copy(b[17:33], unconnectedMessageSequence[:])
	binary.BigEndian.PutUint16(b[33:35], uint16(len(pk.Data))) //nolint:gosec // data length is validated to fit within packet bounds
	copy(b[35:], pk.Data)
	return b
}

// UnmarshalBinary decodes an UnconnectedPong from a raw RakNet packet.
func (pk *UnconnectedPong) UnmarshalBinary(data []byte) error {
	if len(data) < 35 {
		return io.ErrUnexpectedEOF
	}
	pk.SendTimestamp = int64(binary.BigEndian.Uint64(data[1:9])) //nolint:gosec // intentional protocol bit-for-bit conversion
	pk.ServerGUID = int64(binary.BigEndian.Uint64(data[9:17]))   //nolint:gosec // intentional protocol bit-for-bit conversion
	copy(pk.Magic[:], data[17:33])
	dataLen := int(binary.BigEndian.Uint16(data[33:35]))
	if len(data) < 35+dataLen {
		return io.ErrUnexpectedEOF
	}
	pk.Data = append([]byte(nil), data[35:35+dataLen]...)
	return nil
}
