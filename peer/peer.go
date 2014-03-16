package peer

import (
	"errors"
	"fmt"
	"net"
	"time"

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
	IP             string
	Port           int
	Connection     *net.Conn
	Protocol       string
	Status         PeerStatus
	TorrentInfo    *torrent_info.TorrentInfo
	LocalPeerId    string
	RemotePeerId   string
	ExistingPieces []byte
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

func (peer *Peer) WaitForContents(comm chan PeerCommunication) {
	if peer.Status == Connected && peer.Connection != nil {

		// We either receive a 'bitfield' or a 'have' message.
		go (func() {

			bitfieldInfo := make([]byte, (peer.TorrentInfo.FileInformations.PieceCount/8)+1)

			for true {
				id, data, err := peer.TryReadMessage(1 * time.Second)
				if err != nil {
					break
				}

				if id == BITFIELD {
					for index, _ := range data {
						bitfieldInfo[index] |= data[index]
					}
				} else if id == HAVE {
					pieceIndex := bytesToInt(data)
					bitfieldPosition := pieceIndex / 8
					bytePosition := pieceIndex - (bitfieldPosition * 8)
					bitfieldInfo[bitfieldPosition] |= (1 << (7 - byte(bytePosition)))
				}
			}
			comm <- PeerCommunication{peer, bitfieldInfo, "Contents OK"}
			return
		})()
	} else {
		comm <- PeerCommunication{peer, nil, "Error at contents: Peer not connected"}
	}
}

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

func intToBytes(value int) []byte {

	bytes := []byte{0, 0, 0, 0}

	bytes[0] = byte((value >> 24) & 0xFF)
	bytes[1] = byte((value >> 16) & 0xFF)
	bytes[2] = byte((value >> 8) & 0xFF)
	bytes[3] = byte(value & 0xFF)

	return bytes
}

func bytesToInt(bytes []byte) int {
	var number int = 0
	for _, b := range bytes {
		number = (number << 8) + int(b)
	}
	return number
}

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

func (peer *Peer) Disconnect() {

	peer.Status = Disconnected
	if peer.Connection != nil {
		(*peer.Connection).Close()
	}
	peer.Connection = nil
	return
}

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

func New(torrentInfo *torrent_info.TorrentInfo, peerId string, ip string, port int) Peer {
	return Peer{
		IP:          ip,
		Port:        port,
		Status:      Disconnected,
		TorrentInfo: torrentInfo,
		LocalPeerId: peerId,
	}
}