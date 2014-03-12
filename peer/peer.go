package peer

import (
	"bytes"
	"encoding/binary"
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
	IP           string
	Port         int
	Connection   *net.Conn
	Protocol     string
	Status       PeerStatus
	TorrentInfo  *torrent_info.TorrentInfo
	LocalPeerId  string
	RemotePeerId string
}

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

func (peer *Peer) SendUnchoke(comm chan PeerCommunication) {
	if peer.Status == Connected && peer.Connection != nil {
		go (func() {
			buf := []byte{0, 0, 0, 1, 1}

			(*peer.Connection).SetDeadline(time.Now().Add(5 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(buf)

			if err != nil || bytesWritten < len(buf) {

				if err != nil {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at unchoke: %s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at unchoke: %s", "Insufficient bytes written")}
				}
				return
			}
			comm <- PeerCommunication{peer, nil, "Unchoke OK"}
			return
		})()
	}
}

func (peer *Peer) SendInterested(comm chan PeerCommunication) {
	if peer.Status == Connected && peer.Connection != nil {
		go (func() {
			buf := []byte{0, 0, 0, 1, 2}
			(*peer.Connection).SetDeadline(time.Now().Add(5 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(buf)

			if err != nil || bytesWritten < len(buf) {

				if err != nil {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at interested: %s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at interested: %s", "Insufficient bytes written")}
				}
				return
			}
			comm <- PeerCommunication{peer, nil, "Interested OK"}
			return
		})()
	}
}

func (peer *Peer) RequestPiece(comm chan PeerCommunication, index int64, begin int64, length int64) {
	if peer.Status == Connected && peer.Connection != nil {
		go (func() {
			buf := new(bytes.Buffer)
			lens := []byte{0, 0, 0, 13}
			binary.Write(buf, binary.LittleEndian, lens)
			var id int32 = 6
			binary.Write(buf, binary.LittleEndian, id)
			binary.Write(buf, binary.LittleEndian, index)
			binary.Write(buf, binary.LittleEndian, begin)
			binary.Write(buf, binary.LittleEndian, length)

			(*peer.Connection).SetDeadline(time.Now().Add(5 * time.Second))
			bytesWritten, err := (*peer.Connection).Write(buf.Bytes())

			if err != nil || bytesWritten < len(buf.Bytes()) {

				if err != nil {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at request: %s", err)}
				} else {
					comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at request: %s", "Insufficient bytes written")}
				}
				return
			}

			responseBytes := make([]byte, length)
			bytesRead, err := (*peer.Connection).Read(responseBytes)

			if err != nil {
				comm <- PeerCommunication{peer, nil, fmt.Sprintf("Error at request: %s", err)}
			} else {
				comm <- PeerCommunication{peer, responseBytes[0:bytesRead], "Request OK"}
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
