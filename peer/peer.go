package peer

import (
	"bufio"
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
	Peer    *Peer
	Message string
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

func (peer *Peer) Handshake(comm chan PeerCommunication) {
	if peer.Status == Disconnected || peer.Connection == nil {
		peer.connect(func(err error) {
			if err == nil {
				peer.Status = PendingHandshake
				peer.Handshake(comm)
			} else {
				comm <- PeerCommunication{peer, fmt.Sprintf("Error: %s", err)}
			}
		})
		return
	} else if peer.Status != PendingHandshake {
		comm <- PeerCommunication{peer, fmt.Sprintf("Error: Invalid status: %d", peer.Status)}
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

		(*peer.Connection).Write(handshake)

		resp := make([]byte, len(handshake))
		_, err := bufio.NewReader(*peer.Connection).Read(resp)
		if err != nil {
		
			peer.Status = Disconnected
			peer.Connection = nil
			comm <- PeerCommunication{peer, fmt.Sprintf("Error: %s", err)}
			return
		}

		protocol := resp[1:20]
		if string(protocol) != protocolString {
			peer.Status = Disconnected
			peer.Connection = nil
			comm <- PeerCommunication{peer, fmt.Sprintf("Wrong protocol: %s", string(protocol))}
			return
		}
		remotePeerId := string(resp[48:])

		peer.Protocol = string(protocol)
		peer.Status = Connected
		peer.RemotePeerId = remotePeerId

		comm <- PeerCommunication{peer, "OK"}
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
