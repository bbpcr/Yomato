package peer

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/bbpcr/Yomato/bitfield"
	"github.com/bbpcr/Yomato/torrent_info"
)

type PeerStatus int

const (
	DISCONNECTED PeerStatus = iota
	PENDING_HANDSHAKE
	HANDSHAKED
	CONNECTED
)

type PeerCommunication struct {
	Peer          *Peer
	BytesReceived []byte
	MessageID     int
	StatusMessage string
}

type Peer struct {
	IP           string
	Port         int
	Connection   *net.Conn
	Protocol     string
	Status       PeerStatus
	TorrentInfo  *torrent_info.TorrentInfo
	LocalPeerId  string
	RemotePeerId string
	BitfieldInfo bitfield.Bitfield
}

const (
	CHOKE           = 0
	UNCHOKE         = 1
	INTERESTED      = 2
	NOT_INTERESTED  = 3
	HAVE            = 4
	BITFIELD        = 5
	REQUEST         = 6
	PIECE           = 7
	CANCEL          = 8
	PORT            = 9
	HANDSHAKE       = 10
	FULL_CONNECTION = 11
)

// GetInfo return a string consisting of peer status
func (peer *Peer) GetInfo() string {
	infoString := fmt.Sprintf("Remote IP : %s:%d", peer.IP, peer.Port)
	infoString += fmt.Sprintln()
	infoString += fmt.Sprintln("Remote peer ID : ", peer.RemotePeerId)
	infoString += fmt.Sprintln("Remote peer ID length : ", len(peer.RemotePeerId))
	infoString += fmt.Sprintln("Protocol : ", peer.Protocol)
	switch peer.Status {
	case DISCONNECTED:
		infoString += fmt.Sprintln("Status : DISCONNECTED")
	case CONNECTED:
		infoString += fmt.Sprintln("Status : CONNECTED")
	case PENDING_HANDSHAKE:
		infoString += fmt.Sprintln("Status : Pending Handshake")
	default:
		infoString += fmt.Sprintln("Status : NONE")
	}
	infoString += fmt.Sprintln("Local peer ID : ", peer.LocalPeerId)
	return infoString
}

// connect tries to get a TCP then an UDP connection for a peer
func (peer *Peer) connect(callback func(error)) {
	go (func() {
		for _, protocol := range []string{"tcp", "udp"} {
			conn, err := net.DialTimeout(protocol, fmt.Sprintf("%s:%d", peer.IP, peer.Port), 1*time.Second)
			if err != nil {
				continue
			}
			peer.Connection = &conn
			callback(nil)
			return
		}
		callback(errors.New("Peer not available"))
	})()
}

// TryReadMessage returns (type of messasge, message, error) received by a peer
func (peer *Peer) TryReadMessage(timeout time.Duration) (int, []byte, error) {

	//First we read the first 5 bytes;
	(*peer.Connection).SetReadDeadline(time.Now().Add(timeout))
	readBuffer := []byte{0, 0, 0, 0, 0}
	bytesRead, err := (*peer.Connection).Read(readBuffer)

	if err != nil || bytesRead < len(readBuffer) {
		if err != nil {
			return -1, nil, err
		} else {
			return -1, nil, errors.New("Insufficient readed")
		}
	}

	// Then we convert the first 4 bytes into length , 5-th byte into id , and we read the rest of the data

	length := bytesToInt(readBuffer[0:4])
	id := int(readBuffer[4])

	var writeBuffer []byte
	remainingBytes := length - 1
	const BUFFER_SIZE = 1024 * 100 // 100Kb

	for remainingBytes > 0 {

		var newBuffer []byte
		if BUFFER_SIZE < remainingBytes {
			newBuffer = make([]byte, BUFFER_SIZE)
		} else {
			newBuffer = make([]byte, remainingBytes)
		}

		bytesRead, err = (*peer.Connection).Read(newBuffer)
		if err != nil {
			return -1, nil, err
		}
		writeBuffer = append(writeBuffer, newBuffer[0:bytesRead]...)
		remainingBytes -= bytesRead
	}
	return id, writeBuffer, nil
}

// WaitForContents sends to channel comm information about downloaded content of a peer
func (peer *Peer) ReadExistingPieces(comm chan PeerCommunication) {
	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {

		// We either receive a 'bitfield' or a 'have' message.
		go (func() {

			bitfieldInfo := bitfield.New(int(peer.TorrentInfo.FileInformations.PieceCount))

			for true {
				id, data, err := peer.TryReadMessage(1 * time.Second)
				if err != nil {
					break
				}

				if id == BITFIELD {
					bitfieldInfo.Put(data, len(data))
				} else if id == HAVE {
					pieceIndex := bytesToInt(data)
					bitfieldInfo.Set(pieceIndex, true)
				}
			}

			peer.BitfieldInfo = bitfieldInfo
			comm <- PeerCommunication{peer, peer.BitfieldInfo.Encode(), BITFIELD, "OK"}
			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, BITFIELD, "Error:Peer not connected"}
	}
}

// SendUnchoke sends to the peer to unchoke
func (peer *Peer) SendUnchoke(comm chan PeerCommunication) {
	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {
		go (func() {
			buf := []byte{0, 0, 0, 1, 1}

			(*peer.Connection).SetWriteDeadline(time.Now().Add(1 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(buf)

			if err != nil || bytesWritten < len(buf) {

				if err != nil {
					comm <- PeerCommunication{peer, nil, UNCHOKE, fmt.Sprintf("Error at unchoke: %s", err)}
					comm <- PeerCommunication{peer, nil, UNCHOKE, fmt.Sprintf("Error at unchoke: %s", "Insufficient bytes written")}
				}
				return
			}
			comm <- PeerCommunication{peer, nil, UNCHOKE, "Unchoked OK"}
			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, UNCHOKE, "Error at unchoke: Peer not connected"}
	}
}

// SendInterested sends to the peer through the main channel that it's interested
// Data transfer takes place whenever one side is interested and the other side is not choking
func (peer *Peer) SendInterested(comm chan PeerCommunication) {
	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {
		go (func() {
			buf := []byte{0, 0, 0, 1, 2}
			(*peer.Connection).SetWriteDeadline(time.Now().Add(1 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(buf)

			if err != nil || bytesWritten < len(buf) {
				if err != nil {
					comm <- PeerCommunication{peer, nil, INTERESTED, fmt.Sprintf("Error:%s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, INTERESTED, fmt.Sprintf("Error:%s", "Insufficient bytes written")}
				}
				return
			}

			id, data, err := peer.TryReadMessage(2 * time.Second)

			if err != nil || id != UNCHOKE {
				if err != nil {
					comm <- PeerCommunication{peer, nil, INTERESTED, fmt.Sprintf("Error:%s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, INTERESTED, fmt.Sprintf("Error:Didn't receive unchoked")}
				}
				return
			}
			comm <- PeerCommunication{peer, data, INTERESTED, "OK"}

			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, INTERESTED, "Error:Peer not connected"}
	}
}

// intToBytes converts value to an array of bytes and returns it
func intToBytes(value int) []byte {

	bytes := []byte{0, 0, 0, 0}

	bytes[0] = byte((value >> 24) & 0xFF)
	bytes[1] = byte((value >> 16) & 0xFF)
	bytes[2] = byte((value >> 8) & 0xFF)
	bytes[3] = byte(value & 0xFF)

	return bytes
}

// bytesToInt converts bytes to an integer and returns it.
func bytesToInt(bytes []byte) int {
	var number int = 0
	for _, b := range bytes {
		number = (number << 8) + int(b)
	}
	return number
}

// RequestPiece makes a request to tracker to obtain 'length' bytes from 'begin'
// the data is sent to channel 'comm'.
func (peer *Peer) RequestPiece(comm chan PeerCommunication, index int, begin int, length int) {

	if length+begin > int(peer.TorrentInfo.FileInformations.PieceLength) {
		length = int(peer.TorrentInfo.FileInformations.PieceLength) - begin
	}

	if index == int(peer.TorrentInfo.FileInformations.PieceCount-1) {
		if peer.TorrentInfo.FileInformations.PieceCount >= 2 {
			lastPieceLength := peer.TorrentInfo.FileInformations.TotalLength - peer.TorrentInfo.FileInformations.PieceLength*(peer.TorrentInfo.FileInformations.PieceCount-1)
			if begin+length > int(lastPieceLength) {
				length = int(lastPieceLength) - begin
			}
		}
	}

	bytesToBeWritten := []byte{0, 0, 0, 13, 6}
	bytesToBeWritten = append(bytesToBeWritten, intToBytes(index)...)
	bytesToBeWritten = append(bytesToBeWritten, intToBytes(begin)...)
	bytesToBeWritten = append(bytesToBeWritten, intToBytes(length)...)

	if (peer.Status == HANDSHAKED || peer.Status == CONNECTED) && peer.Connection != nil {
		go (func() {

			(*peer.Connection).SetWriteDeadline(time.Now().Add(1 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(bytesToBeWritten)

			if err != nil || bytesWritten < len(bytesToBeWritten) {

				if err != nil {
					comm <- PeerCommunication{peer, bytesToBeWritten[5:], REQUEST, fmt.Sprintf("Error:%s", err)}
				} else {
					comm <- PeerCommunication{peer, bytesToBeWritten[5:], REQUEST, fmt.Sprintf("Error:%s", "Insufficient bytes written")}
				}
				return
			}

			id, data, err := peer.TryReadMessage(2 * time.Second)

			if err != nil || id != PIECE {
				comm <- PeerCommunication{peer, bytesToBeWritten[5:], REQUEST, fmt.Sprintf("Error:%s", err)}
			} else {
				comm <- PeerCommunication{peer, data, REQUEST, "OK"}
			}
			return
		})()
	} else {
		comm <- PeerCommunication{peer, bytesToBeWritten[5:], REQUEST, "Error:Peer not connected"}
	}
}

// Disconnect closes the connection of a peer.
func (peer *Peer) Disconnect() {

	peer.Status = DISCONNECTED
	if peer.Connection != nil {
		(*peer.Connection).Close()
	}
	peer.Connection = nil
	return
}

// Handshake attempts to set up the first message transmitted by the peer, sending the response through comm
func (peer *Peer) Handshake(comm chan PeerCommunication) {
	if peer.Status == DISCONNECTED || peer.Connection == nil {
		peer.connect(func(err error) {
			if err == nil {
				peer.Status = PENDING_HANDSHAKE
				peer.Handshake(comm)
			} else {

				peer.Disconnect()
				comm <- PeerCommunication{peer, nil, HANDSHAKE, fmt.Sprintf("Error:%s", err)}
			}
		})
		return
	} else if peer.Status != PENDING_HANDSHAKE {
		comm <- PeerCommunication{peer, nil, HANDSHAKE, fmt.Sprintf("Error:Invalid status %d", peer.Status)}
		return
	}

	go (func() {
		protocolString := "BitTorrent protocol"
		handshake := make([]byte, 0, 48+len(protocolString))
		handshake = append(handshake, byte(19))
		handshake = append(handshake, []byte(protocolString)...)
		handshake = append(handshake, []byte{0, 0, 0, 0, 0, 0, 0, 0}...)
		handshake = append(handshake, peer.TorrentInfo.InfoHash...)
		handshake = append(handshake, []byte(peer.LocalPeerId)...)

		(*peer.Connection).SetDeadline(time.Now().Add(5 * time.Second))
		bytesWritten, err := (*peer.Connection).Write(handshake)

		if err != nil || bytesWritten < len(handshake) {

			if err != nil {
				comm <- PeerCommunication{peer, nil, HANDSHAKE, fmt.Sprintf("Error:%s", err)}
			} else {
				comm <- PeerCommunication{peer, nil, HANDSHAKE, fmt.Sprintf("Error:%s", "Insufficient bytes written")}
			}
			peer.Disconnect()

			return
		}

		resp := make([]byte, len(handshake))
		bytesRead, err := (*peer.Connection).Read(resp)

		if err != nil || bytesRead < len(resp) {

			peer.Disconnect()
			if err != nil {
				comm <- PeerCommunication{peer, nil, HANDSHAKE, fmt.Sprintf("Error:%s", err)}
			} else {
				comm <- PeerCommunication{peer, nil, HANDSHAKE, fmt.Sprintf("Error:%s", "Insufficient bytes read")}
			}

			return
		}

		protocol := resp[1:20]
		peer.Protocol = string(protocol)
		if string(protocol) != protocolString {

			peer.Disconnect()
			comm <- PeerCommunication{peer, nil, HANDSHAKE, fmt.Sprintf("Error:Wrong protocol %s", string(protocol))}
			return
		}

		remotePeerId := string(resp[48:])

		peer.Status = HANDSHAKED
		peer.RemotePeerId = remotePeerId
		comm <- PeerCommunication{peer, resp, HANDSHAKE, "OK"}
	})()
}

func (peer *Peer) EstablishFullConnection(comm chan PeerCommunication) {

	go (func() {
		tempChan := make(chan PeerCommunication)
		peer.Handshake(tempChan)

		msg, _ := <-tempChan
		if msg.StatusMessage != "OK" {
			comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "Error:Unable to handshake"}
			return
		}

		//fmt.Println(peer.RemotePeerId, " passed HANDSHAKE")

		peer.ReadExistingPieces(tempChan)
		msg, _ = <-tempChan
		if msg.StatusMessage != "OK" {
			comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "Error:Unable to read the bitfield"}
			return
		}

		//fmt.Println(peer.RemotePeerId, " passed BITFIELD")

		peer.SendUnchoke(tempChan)
		msg, _ = <-tempChan
		if msg.StatusMessage != "OK" {
		}

		//fmt.Println(peer.RemotePeerId, " passed UNCHOKE")

		peer.SendInterested(tempChan)
		msg, _ = <-tempChan
		if msg.StatusMessage != "OK" {
		}

		peer.Status = CONNECTED
		comm <- PeerCommunication{peer, nil, FULL_CONNECTION, "OK"}
		return
	})()
}

// New returns a peer with given description
func New(torrentInfo *torrent_info.TorrentInfo, peerId string, ip string, port int) Peer {
	return Peer{
		IP:          ip,
		Port:        port,
		Status:      DISCONNECTED,
		TorrentInfo: torrentInfo,
		LocalPeerId: peerId,
	}
}
