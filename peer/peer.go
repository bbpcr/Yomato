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
	Disconnected PeerStatus = iota
	PendingHandshake
	Connected
)

type PeerCommunication struct {
	Peer          *Peer
	BytesReceived []byte
	Message       string
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
	CHOKE          = 0
	UNCHOKE        = 1
	INTERESTED     = 2
	NOT_INTERESTED = 3
	HAVE           = 4
	BITFIELD       = 5
	REQUEST        = 6
	PIECE          = 7
	CANCEL         = 8
	PORT           = 9
)

// GetInfo return a string consisting of peer status
func (peer *Peer) GetInfo() string {
	infoString := fmt.Sprintf("Remote IP : %s:%d", peer.IP, peer.Port)
	infoString += fmt.Sprintln()
	infoString += fmt.Sprintln("Remote peer ID : ", peer.RemotePeerId)
	infoString += fmt.Sprintln("Remote peer ID length : ", len(peer.RemotePeerId))
	infoString += fmt.Sprintln("Protocol : ", peer.Protocol)
	switch peer.Status {
	case Disconnected:
		infoString += fmt.Sprintln("Status : Disconnected")
	case Connected:
		infoString += fmt.Sprintln("Status : Connected")
	case PendingHandshake:
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
	if peer.Status == Connected && peer.Connection != nil {

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
			comm <- PeerCommunication{peer, peer.BitfieldInfo.Encode(), "Contents OK"}
			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, "Error at contents: Peer not connected"}
	}
}

// SendUnchoke sends to the peer to unchoke
func (peer *Peer) SendUnchoke(comm chan PeerCommunication) {
	if peer.Status == Connected && peer.Connection != nil {
		go (func() {
			buf := []byte{0, 0, 0, 1, 1}

			(*peer.Connection).SetWriteDeadline(time.Now().Add(1 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(buf)

			if err != nil || bytesWritten < len(buf) {

				if err != nil {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at unchoke: %s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at unchoke: %s", "Insufficient bytes written")}
				}
				return
			}
			comm <- PeerCommunication{peer, nil, "Unchoked OK"}
			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, "Error at unchoke: Peer not connected"}
	}
}

// SendInterested sends to the peer through the main channel that it's interested
// Data transfer takes place whenever one side is interested and the other side is not choking
func (peer *Peer) SendInterested(comm chan PeerCommunication) {
	if peer.Status == Connected && peer.Connection != nil {
		go (func() {
			buf := []byte{0, 0, 0, 1, 2}
			(*peer.Connection).SetWriteDeadline(time.Now().Add(1 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(buf)

			if err != nil || bytesWritten < len(buf) {
				if err != nil {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at interested: %s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at interested: %s", "Insufficient bytes written")}
				}
				return
			}

			id, data, err := peer.TryReadMessage(2 * time.Second)

			if err != nil || id != UNCHOKE {
				if err != nil {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at interested: %s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at interested: %s", "Didn't receive unchoked")}
				}
				return
			}
			comm <- PeerCommunication{peer, data, "Interested OK"}

			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, "Error at interested: Peer not connected"}
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
	if peer.Status == Connected && peer.Connection != nil {
		go (func() {
			bytesToBeWritten := []byte{0, 0, 0, 13, 6}
			bytesToBeWritten = append(bytesToBeWritten, intToBytes(index)...)
			bytesToBeWritten = append(bytesToBeWritten, intToBytes(begin)...)
			bytesToBeWritten = append(bytesToBeWritten, intToBytes(length)...)

			(*peer.Connection).SetWriteDeadline(time.Now().Add(1 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(bytesToBeWritten)

			if err != nil || bytesWritten < len(bytesToBeWritten) {

				if err != nil {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at request: %s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at request: %s", "Insufficient bytes written")}
				}
				return
			}

			id, data, err := peer.TryReadMessage(2 * time.Second)

			if err != nil || id != PIECE {
				comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at request: %s", err)}
			} else {
				comm <- PeerCommunication{peer, data, "Request OK"}
			}
			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, "Error at request: Peer not connected"}
	}
}

// Disconnect closes the connection of a peer.
func (peer *Peer) Disconnect() {

	peer.Status = Disconnected
	if peer.Connection != nil {
		(*peer.Connection).Close()
	}
	peer.Connection = nil
	return
}

// Handshake attempts to set up the first message transmitted by the peer, sending the response through comm
func (peer *Peer) Handshake(comm chan PeerCommunication) {
	if peer.Status == Disconnected || peer.Connection == nil {
		peer.connect(func(err error) {
			if err == nil {
				peer.Status = PendingHandshake
				peer.Handshake(comm)
			} else {

				peer.Disconnect()
				comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at handshake: %s", err)}
			}
		})
		return
	} else if peer.Status != PendingHandshake {
		comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at handshake: Invalid status: %d", peer.Status)}
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
				comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at sending handshake: %s", err)}
			} else {
				comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at sending handshake: %s", "Insufficient bytes written")}
			}
			peer.Disconnect()

			return
		}

		resp := make([]byte, len(handshake))
		bytesRead, err := (*peer.Connection).Read(resp)

		if err != nil || bytesRead < len(resp) {

			peer.Disconnect()
			if err != nil {
				comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at receiving handshake: %s", err)}
			} else {
				comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at receiving handshake: %s", "Insufficient bytes read")}
			}

			return
		}

		protocol := resp[1:20]
		peer.Protocol = string(protocol)
		if string(protocol) != protocolString {

			peer.Disconnect()
			comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at handshake: Wrong protocol %s", string(protocol))}
			return
		}

		remotePeerId := string(resp[48:])

		peer.Status = Connected
		peer.RemotePeerId = remotePeerId
		comm <- PeerCommunication{peer, resp, "Handshake OK"}
	})()
}

// New returns a peer with given description
func New(torrentInfo *torrent_info.TorrentInfo, peerId string, ip string, port int) Peer {
	return Peer{
		IP:          ip,
		Port:        port,
		Status:      Disconnected,
		TorrentInfo: torrentInfo,
		LocalPeerId: peerId,
	}
}
