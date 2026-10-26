package raknet

// Most of this code is based on the RakNet protocol offline messages
// https://github.com/Sandertv/go-raknet

import (
	"encoding/binary"
	"io"
)

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

func (pk *UnconnectedPing) UnmarshalBinary(data []byte) error {
	if len(data) < 33 {
		return io.ErrUnexpectedEOF
	}
	pk.SendTimestamp = int64(binary.BigEndian.Uint64(data[1:9]))
	copy(pk.Magic[:], data[9:25])
	pk.ClientGUID = int64(binary.BigEndian.Uint64(data[25:33]))
	return nil
}

// UnconnectedPong represents an offline pong packet sent by servers responding to pings.
type UnconnectedPong struct {
	SendTimestamp int64
	ServerGUID    int64
	Magic         [16]byte
	Data          []byte
}

func (pk *UnconnectedPong) MarshalBinary() []byte {
	b := make([]byte, 35+len(pk.Data))
	b[0] = IDUnconnectedPong
	binary.BigEndian.PutUint64(b[1:9], uint64(pk.SendTimestamp))
	binary.BigEndian.PutUint64(b[9:17], uint64(pk.ServerGUID))
	copy(b[17:33], unconnectedMessageSequence[:])
	binary.BigEndian.PutUint16(b[33:35], uint16(len(pk.Data)))
	copy(b[35:], pk.Data)
	return b
}

func (pk *UnconnectedPong) UnmarshalBinary(data []byte) error {
	if len(data) < 35 {
		return io.ErrUnexpectedEOF
	}
	pk.SendTimestamp = int64(binary.BigEndian.Uint64(data[1:9]))
	pk.ServerGUID = int64(binary.BigEndian.Uint64(data[9:17]))
	copy(pk.Magic[:], data[17:33])
	dataLen := int(binary.BigEndian.Uint16(data[33:35]))
	if len(data) < 35+dataLen {
		return io.ErrUnexpectedEOF
	}
	pk.Data = append([]byte(nil), data[35:35+dataLen]...)
	return nil
}
