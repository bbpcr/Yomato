package peer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/bbpcr/Yomato/bitfield"
	"github.com/bbpcr/Yomato/file_writer"
	"github.com/bbpcr/Yomato/torrent_info"
)

type PeerStatus int

const (
	DISCONNECTED PeerStatus = iota
	CONNECTED
)

type ConnectionCommunication struct {
	Peer          *Peer
	StatusMessage string
	Duration      time.Duration
}

type Peer struct {
	IP           string
	Port         int
	Connection   *net.TCPConn
	Protocol     string
	Status       PeerStatus
	TorrentInfo  *torrent_info.TorrentInfo
	LocalPeerId  string
	RemotePeerId string
	BitfieldInfo bitfield.Bitfield

	ClientChoking    bool
	ClientInterested bool
	PeerChoking      bool
	PeerInterested   bool

	Downloading bool
	Active      bool

	ConnectTime time.Duration
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
	HANDSHAKE      = 10
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
	default:
		infoString += fmt.Sprintln("Status : NONE")
	}
	infoString += fmt.Sprintln("Local peer ID : ", peer.LocalPeerId)
	return infoString
}
func (peer *Peer) connect() error {
	tcpAdress, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", peer.IP, peer.Port))
	if err != nil {
		return err
	}
	tcpConnection, err := net.DialTCP("tcp", nil, tcpAdress)
	if err != nil {
		return err
	}
	tcpConnection.SetKeepAlive(true)
	tcpConnection.SetNoDelay(false)
	tcpConnection.SetReadBuffer(64 * 1024)
	tcpConnection.SetLinger(0)
	peer.Connection = tcpConnection
	return nil
}

func readExactly(connection *net.TCPConn, buffer []byte, length int) error {
	bytesReaded := 0

	if length > len(buffer) || length < 0 {
		return errors.New("Invalid parameters")
	}
	for bytesReaded < length {
		readed, err := connection.Read(buffer[bytesReaded:length])
		if err != nil {
			return err
		}
		bytesReaded += readed
	}
	return nil
}

func writeExactly(connection *net.TCPConn, buffer []byte, length int) error {

	if length > len(buffer) || length < 0 {
		return errors.New("Invalid parameters")
	}

	bytesWritten := 0
	connection.SetWriteDeadline(time.Now().Add(1 * time.Second))
	for bytesWritten < length {
		written, err := connection.Write(buffer[bytesWritten:length])
		if err != nil {
			return err
		}
		bytesWritten += written
	}
	return nil

}

// tryReadMessage returns (type of messasge, message, error) received by a peer
func (peer *Peer) tryReadMessage(timeout time.Duration, maxBufferSize int) (int, []byte, error) {

	if peer.Status != CONNECTED {
		return -1, nil, errors.New("Peer not connected")
	}
	// First we read the first 5 bytes;
	if timeout == 0 {
		peer.Connection.SetReadDeadline(time.Time{})
	} else {
		peer.Connection.SetReadDeadline(time.Now().Add(timeout))
	}

	buffer := make([]byte, maxBufferSize)
	err := readExactly(peer.Connection, buffer, 5)

	if err != nil {
		return -1, nil, err
	}

	// Then we convert the first 4 bytes into length , 5-th byte into id , and we read the rest of the data

	length := int(binary.BigEndian.Uint32(buffer[0:4]))
	id := int(buffer[4])

	err = readExactly(peer.Connection, buffer, length-1)

	if err != nil {
		return -1, nil, err
	}
	return id, buffer[0 : length-1], nil
}

func (peer *Peer) sendBitfield(bitfieldBytes []byte) error {
	if peer.Status == CONNECTED {
		id := BITFIELD
		length := 1 + len(bitfieldBytes)
		messageBytes := convertIntsToByteArray(length)
		messageBytes = append(messageBytes, byte(id))
		messageBytes = append(messageBytes, bitfieldBytes...)
		return writeExactly(peer.Connection, messageBytes, len(messageBytes))
	}
	return errors.New("Peer not connected")
}

func (peer *Peer) sendKeepAlive() error {
	if peer.Status == CONNECTED {

		buf := []byte{0, 0, 0, 0}
		return writeExactly(peer.Connection, buf, len(buf))
	}
	return errors.New("Peer not connected")
}

// Sends and unchoke message to the peer
// The message is exactly : [0, 0, 0, 1, 0] (first four bytes length = 1 , last byte the id of the message = 0).
// Peers wont respond to block requests if they are choked and uninterested.
func (peer *Peer) sendChoke() error {
	if peer.Status == CONNECTED {

		buf := []byte{0, 0, 0, 1, CHOKE}
		err := writeExactly(peer.Connection, buf, len(buf))
		if err == nil {
			peer.ClientChoking = true
		}
		return err
	}
	return errors.New("Peer not connected")
}

// Sends and unchoke message to the peer
// The message is exactly : [0, 0, 0, 1, 1] (first four bytes length = 1 , last byte the id of the message = 1).
// Peers wont respond to block requests if they are choked and uninterested.
func (peer *Peer) sendUnchoke() error {

	if peer.Status == CONNECTED {

		buf := []byte{0, 0, 0, 1, UNCHOKE}
		err := writeExactly(peer.Connection, buf, len(buf))
		if err == nil {
			peer.ClientChoking = false
		}
		return err
	}
	return errors.New("Peer not connected")
}

// Sends an interested message to the peer.
// The message is exactly : [0, 0, 0, 1, 2] (first four bytes length = 1 , last byte the id of the message = 2).
// Peers wont respond to block requests if they are choked and uninterested.
func (peer *Peer) sendInterested() error {

	if peer.Status == CONNECTED {

		buf := []byte{0, 0, 0, 1, INTERESTED}
		err := writeExactly(peer.Connection, buf, len(buf))
		if err == nil {
			peer.ClientInterested = true
		}
		return err
	}
	return errors.New("Peer not connected")
}

// This function reads messages , and parses them.
func (peer *Peer) readMessages(maxMessages int, messageTimeoutDuration time.Duration) []file_writer.PieceData {

	pieces := make([]file_writer.PieceData, 0)
	if peer.Status == CONNECTED {

		for messageIndex := 0; messageIndex < maxMessages; messageIndex++ {

			id, data, err := peer.tryReadMessage(messageTimeoutDuration, 17*1024)
			if err != nil {
				break
			}
			if id == BITFIELD {

				peer.BitfieldInfo.Put(data, len(data))
			} else if id == HAVE {

				pieceIndex := int(binary.BigEndian.Uint32(data))
				peer.BitfieldInfo.Set(pieceIndex, true)
			} else if id == UNCHOKE {

				peer.PeerChoking = false
			} else if id == CHOKE {

				peer.PeerChoking = true
				break
			} else if id == INTERESTED {

				peer.PeerInterested = true
			} else if id == NOT_INTERESTED {

				peer.PeerInterested = false
			} else if id == PIECE {

				var pieceData file_writer.PieceData
				pieceData.PieceNumber = int(binary.BigEndian.Uint32(data[0:4]))
				pieceData.Offset = int(binary.BigEndian.Uint32(data[4:8]))
				pieceData.Piece = data[8:]
				pieces = append(pieces, pieceData)
			}
		}
	}
	return pieces
}

func (peer *Peer) ReadMessages(maxMessages int, timeoutDuration time.Duration) []file_writer.PieceData {
	return peer.readMessages(maxMessages, timeoutDuration)
}

// Sends an interested message to the peer.
// The message is exactly : [0, 0, 0, 1, 3] (first four bytes length = 1 , last byte the id of the message = 3).
// Peers wont respond to block requests if they are choked and uninterested.
func (peer *Peer) sendUninterested() error {

	if peer.Status == CONNECTED {

		buf := []byte{0, 0, 0, 1, NOT_INTERESTED}
		err := writeExactly(peer.Connection, buf, len(buf))
		if err == nil {
			peer.ClientInterested = false
		}
		return err
	}
	return errors.New("Peer not connected")

}

// Request multiple blocks on the peers
// Parameters are like this : an array multiple of three,
// [index1, begin1, length1, index2, begin2, length2,...] and so on..
// This writes the byte form of the input on the connection
func (peer *Peer) WriteRequest(params []int) error {
	requestBytes := []byte{}

	// Start creating the bytes for the request

	for request := 0; request < len(params); request += 3 {

		signatureBytes := make([]byte, 5)
		binary.BigEndian.PutUint32(signatureBytes[0:4], 13)
		signatureBytes[4] = REQUEST
		// The first 4 bytes are the length of one request which is 13 = sizeof(ID) + sizeof(index) + sizeof(begin) + sizeof(length)
		requestBytes = append(requestBytes, signatureBytes...)
		requestBytes = append(requestBytes, convertIntsToByteArray(params[request], params[request+1], params[request+2])...)
		// We create one big byte array containing all the requests
	}
	if peer.Status == CONNECTED {
		return writeExactly(peer.Connection, requestBytes, len(requestBytes))
	}
	return errors.New("Peer not connected")
}

// This converts an array of ints into a byte array
// ex : input [567 , 8978] -> output [0 0 2 55 0 0 35 18]
func convertIntsToByteArray(params ...int) []byte {
	buffer := make([]byte, 4*len(params))
	for index := 0; index < 4*len(params); index += 4 {
		binary.BigEndian.PutUint32(buffer[index:index+4], uint32(params[index/4]))
	}
	return buffer
}

// Sends a handshake to the peer.
// This is mandatory to call this first , when initializing a connection with the peer,
// because it won't response to any message until a handshake has been done.
func (peer *Peer) sendHandshake() error {

	if peer.Status == DISCONNECTED {
		//If the peer is disconnected,
		//it connects to the ip and port that we have.
		err := peer.connect()
		if err != nil {
			peer.Disconnect()
			return err
		}

		// At this point , it is connected to the peer.
		// It will send a byte array like this :
		// <protocolStringLength><protocolString><reservedBytes><infoHash><peerID>
		// with exactly (48 + protocolStringLength) bytes

		protocolString := "BitTorrent protocol"
		handshake := make([]byte, 0, 48+len(protocolString))
		handshake = append(handshake, byte(19))
		handshake = append(handshake, []byte(protocolString)...)
		handshake = append(handshake, []byte{0, 0, 0, 0, 0, 0, 0, 0}...)
		handshake = append(handshake, peer.TorrentInfo.InfoHash...)
		handshake = append(handshake, []byte(peer.LocalPeerId)...)

		peer.Connection.SetDeadline(time.Now().Add(5 * time.Second))
		// Set a higher timeout, because some peers respond slower at handshake.
		bytesWritten, err := peer.Connection.Write(handshake)

		if err != nil || bytesWritten < len(handshake) {

			peer.Disconnect()
			if err != nil {
				return err
			} else {
				return errors.New("Insufficient bytes written")
			}
		}

		// At this point , the peer should send us exactly the same size that we requested.
		resp := make([]byte, len(handshake))
		err = readExactly(peer.Connection, resp, len(resp))

		if err != nil {
			peer.Disconnect()
			return err
		}

		// Some peers send wrong protocol , so we disconnect it.
		protocol := resp[1:20]
		peer.Protocol = string(protocol)
		if string(protocol) != protocolString {

			peer.Disconnect()
			return errors.New(fmt.Sprintf("Wrong protocol %s", string(protocol)))
		}

		remotePeerId := string(resp[48:])
		peer.RemotePeerId = remotePeerId
		peer.Status = CONNECTED
		return nil
	}
	return errors.New("Invalid status")
}

func (peer *Peer) SendKeepAlive() error {
	return peer.sendKeepAlive()
}

func (peer *Peer) SendChoke() error {
	return peer.sendChoke()
}

func (peer *Peer) SendUnchoke() error {
	return peer.sendUnchoke()
}

func (peer *Peer) SendInterested() error {
	return peer.sendInterested()
}

func (peer *Peer) SendUninterested() error {
	return peer.sendUninterested()
}

// Disconnect closes the connection of a peer,
// and sets the status to DISCONNECTED.
func (peer *Peer) Disconnect() {

	peer.Status = DISCONNECTED
	if peer.Connection != nil {
		peer.Connection.Close()
	}
	return
}

// Establishes full connection with the peer.
// Full connection means : handshake , reading the bitfield and
// sending unchoke and interested to the peer.
func (peer *Peer) EstablishFullConnection(comm chan ConnectionCommunication, clientBitfield *bitfield.Bitfield) {

	if peer.Status == CONNECTED {
		return
	}
	startTime := time.Now()
	err := peer.sendHandshake()
	if err != nil {
		comm <- ConnectionCommunication{peer, "ERROR:" + err.Error(), time.Since(startTime)}
		return
	}

	err = peer.sendBitfield(clientBitfield.Encode())
	if err != nil {
		peer.Disconnect()
		comm <- ConnectionCommunication{peer, "ERROR:" + err.Error(), time.Since(startTime)}
		return
	}

	err = peer.sendUnchoke()
	if err != nil {
		peer.Disconnect()
		comm <- ConnectionCommunication{peer, "ERROR:" + err.Error(), time.Since(startTime)}
		return
	}

	err = peer.sendInterested()
	if err != nil {
		peer.Disconnect()
		comm <- ConnectionCommunication{peer, "ERROR:" + err.Error(), time.Since(startTime)}
		return
	}

	peer.readMessages(int(peer.TorrentInfo.FileInformations.PieceCount+1), 1*time.Second)

	peer.Status = CONNECTED
	peer.ConnectTime = time.Since(startTime)
	comm <- ConnectionCommunication{peer, "OK", time.Since(startTime)}
	return
}

// New returns a peer with given description
func New(torrentInfo *torrent_info.TorrentInfo, peerId string, ip string, port int) Peer {
	return Peer{
		IP:               ip,
		Port:             port,
		Status:           DISCONNECTED,
		TorrentInfo:      torrentInfo,
		LocalPeerId:      peerId,
		BitfieldInfo:     bitfield.New(int(torrentInfo.FileInformations.PieceCount)),
		ClientChoking:    true,
		ClientInterested: false,
		PeerChoking:      true,
		PeerInterested:   false,
		Downloading:      false,
		Active:           false,
		ConnectTime:      time.Second * 10000,
	}
}
