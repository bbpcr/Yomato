//Package tracker implements basic functionalities offered by a Torrent Tracker
package tracker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/bbpcr/Yomato/bencode"
	"github.com/bbpcr/Yomato/torrent_info"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
)

type Tracker struct {
	TorrentInfo *torrent_info.TorrentInfo
	PeerId      string
	LocalServer *http.Server
	Port        int
}

// readPeersFromAnnouncer returns peers from announceUrl
func readPeersFromAnnouncer(announceUrl string, peerID string, infoHash string, port int, uploaded int64, downloaded int64, left int64) (bencode.Bencoder, error) {
	protocol := announceUrl[0:3]
	if protocol == "htt" {

		qs := url.Values{}
		qs.Add("peer_id", peerID)
		qs.Add("info_hash", infoHash)
		qs.Add("port", fmt.Sprintf("%d", port))
		qs.Add("uploaded", fmt.Sprintf("%d", uploaded))
		qs.Add("downloaded", fmt.Sprintf("%d", downloaded))
		qs.Add("left", fmt.Sprintf("%d", left))
		qs.Add("event", "started")

		response, err := http.Get(announceUrl + "?" + qs.Encode())

		if err != nil {
			return bencode.Dictionary{}, err
		}

		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return bencode.Dictionary{}, err
		}

		response.Body.Close()

		if response.StatusCode == 200 {
			data, _, err := bencode.Parse(body)
			if err != nil {
				return bencode.Dictionary{}, err
			}

			newData, err := GetPeers(data)

			if err != nil {
				return bencode.Dictionary{}, err
			}

			return newData, nil
		}
		return bencode.Dictionary{}, errors.New(fmt.Sprintf("Expected 200 OK from tracker; got %s", response.Status))
	} else if protocol == "udp" {

		adress, err := net.ResolveUDPAddr("udp", announceUrl[6:])
		if err != nil {
			return bencode.Dictionary{}, err
		}
		fmt.Println(adress)
		udpConnection, err := net.DialUDP("udp", nil, adress)
		if err != nil {
			return bencode.Dictionary{}, err
		}
		// http://code.google.com/p/udpt/wiki/UDPTrackerProtocol

		// First step. We make ourself known.
		senderConnectionID := make([]byte, 8)
		binary.BigEndian.PutUint64(senderConnectionID, 0x41727101980)
		senderAction := make([]byte, 4)
		binary.BigEndian.PutUint32(senderAction, 0)
		senderTransactionID := make([]byte, 4)
		binary.BigEndian.PutUint32(senderTransactionID, 1000)
		connectionRequestBytes := senderConnectionID
		connectionRequestBytes = append(connectionRequestBytes, senderAction...)
		connectionRequestBytes = append(connectionRequestBytes, senderTransactionID...)

		udpConnection.SetWriteDeadline(time.Now().Add(1 * time.Second))
		_, err = udpConnection.Write(connectionRequestBytes)
		if err != nil {
			return bencode.Dictionary{}, err
		}

		//Step two , we read from the server. We should receive exactly the same format.
		// First 4 bytes action , next 4 bytes transaction id , last 8 bytes connection id which we will use it next

		udpConnection.SetReadDeadline(time.Now().Add(2 * time.Second))
		buffer := make([]byte, 16)
		bytesReceived, err := udpConnection.Read(buffer)
		if err != nil || bytesReceived < 16 {
			return bencode.Dictionary{}, err
		}

		receivedAction := binary.BigEndian.Uint32(buffer[0:4])
		receivedTransactionID := binary.BigEndian.Uint32(buffer[4:8])
		receivedConnectionID := binary.BigEndian.Uint64(buffer[8:16])
		if receivedAction != 0 || receivedTransactionID != 1000 {
			return bencode.Dictionary{}, errors.New("Unable to make a connection with the udp server")
		}

		//At this point we have connected succesfully to the udp server.Now we can begin to request peers

		//Step three , we send the request for peers.

		/*
			Offset      Size				Name				Value
				0	 8 (64 bit integer)	 connection  id	 connection id from server
				8	 4 (32-bit integer)	 action	 1;  for announce request
				12	 4 (32-bit integer)	 transaction id	 client can make up another transaction id...
				16	 20	                 info_hash	 the info_hash of the torrent that is being announced
				36	 20	                 peer id	 the peer ID of the client announcing itself
				56	 8 (64 bit integer)	 downloaded	 bytes downloaded by client this session
				64	 8 (64 bit integer)	 left	     bytes left to complete the download
				72	 8 (64 bit integer)	 uploaded	 bytes uploaded this session
				80	 4 (32 bit integer)	 event	     0=None; 1=Download completed; 2=Download started; 3=Download stopped.
		*/

		var peerRequest []byte
		ConnectionID := make([]byte, 8)
		binary.BigEndian.PutUint64(ConnectionID, receivedConnectionID)
		peerRequest = append(peerRequest, ConnectionID...)
		Action := []byte{0, 0, 0, 1}
		peerRequest = append(peerRequest, Action...)
		peerRequest = append(peerRequest, senderTransactionID...)
		peerRequest = append(peerRequest, []byte(infoHash)...)
		peerRequest = append(peerRequest, []byte(peerID)...)
		numBuffer := make([]byte, 8)
		binary.BigEndian.PutUint64(numBuffer, uint64(downloaded))
		peerRequest = append(peerRequest, numBuffer...)
		binary.BigEndian.PutUint64(numBuffer, uint64(left))
		peerRequest = append(peerRequest, numBuffer...)
		binary.BigEndian.PutUint64(numBuffer, uint64(uploaded))
		peerRequest = append(peerRequest, numBuffer...)
		Event := []byte{0, 0, 0, 2}
		peerRequest = append(peerRequest, Event...)
		IPV4 := []byte{0, 0, 0, 0}
		peerRequest = append(peerRequest, IPV4...)
		Key := []byte{0, 0, 128, 128}
		peerRequest = append(peerRequest, Key...)
		peerRequest = append(peerRequest, []byte{255, 255, 255, 255, 0, 80}...)

		bytesWritten, err := udpConnection.Write(peerRequest)
		if err != nil || bytesWritten < len(peerRequest) {
			return bencode.Dictionary{}, err
		}

		// Step four. We read the response from server
		/*
			Offset	   Size	        Name	         Value

			0	         4	        action	         1
			4	         4	        transaction id	 same like the transaction id sent be the announce request
			8	         4	        interval	     seconds to wait till next announce
			12	         4	        leechers	     amount of leechers in swarm
			16	         4	        seeders	         amount of seeders in swarm
			20 + 6 * n	 4	        IPv4	         IP of peer
			24 + 6 * n	 2	        port	         TCP port of client
		*/

		// UDP connection doesnt allow me to read buffered, in go it seems, so i try to read in a biiiiig buffer
		// and witch can hold exactly a maximum of 10000 Peers
		const MAX_PEERS = 10000
		bigBuffer := make([]byte, 5*4+MAX_PEERS*6)
		bytesRead, err := udpConnection.Read(bigBuffer)
		bigBuffer = bigBuffer[0:bytesRead]
		firstBytes := bigBuffer[0:20]

		if err != nil {
			return bencode.Dictionary{}, err
		}

		// At this point we have all we need so we create the dictionary from scratch.

		peersAction := binary.BigEndian.Uint32(firstBytes[0:4])
		peersTransactionID := binary.BigEndian.Uint32(firstBytes[4:8])
		peersAnnounceInterval := binary.BigEndian.Uint32(firstBytes[8:12])
		//peersLeechers := binary.BigEndian.Uint32(firstBytes[12:16])
		//peersSeeders := binary.BigEndian.Uint32(firstBytes[16:20])
		//totalPeers := int(peersLeechers + peersSeeders)

		if peersTransactionID != 1000 || peersAction != 1 {
			return bencode.Dictionary{}, errors.New("Incorrect transaction ID or action")
		}

		bigDictionary := new(bencode.Dictionary)
		bigDictionary.Values = make(map[bencode.String]bencode.Bencoder)

		announceInterval := new(bencode.String)
		announceInterval.Value = fmt.Sprintf("%d", peersAnnounceInterval)
		bigDictionary.Values[bencode.String{"interval"}] = announceInterval

		peersList := new(bencode.String)
		peersList.Value = string(bigBuffer[20:])
		bigDictionary.Values[bencode.String{"peers"}] = peersList

		perfectDictionary, err := GetPeers(bigDictionary)

		if err != nil {
			return bencode.Dictionary{}, errors.New("Malformed dictionary")
		}
		return perfectDictionary, nil
		defer udpConnection.Close()
	}
	return bencode.Dictionary{}, errors.New("No known protocol")
}

// RequestPeers encodes an URL, making a request to announcer then
// returns the peers bencoded from readPeersFromAnnouncer
func (tracker Tracker) RequestPeers(bytesUploaded, bytesDownloaded, bytesLeft int64) (bencode.Bencoder, error) {
	peerId := tracker.PeerId

	// We create the URL like this :
	// announcer?peer_id= & info_hash= & port= & uploaded= & downloaded= & left= & event=
	// The uploaded , downloaded and left should always be , but are not necesary

	data, err := readPeersFromAnnouncer(tracker.TorrentInfo.AnnounceUrl, peerId, string(tracker.TorrentInfo.InfoHash), tracker.Port, bytesUploaded, bytesDownloaded, bytesLeft)
	if err == nil {
		return data, nil
	}

	for _, anotherAnnounceUrl := range tracker.TorrentInfo.AnnounceList {
		data, err := readPeersFromAnnouncer(anotherAnnounceUrl, peerId, string(tracker.TorrentInfo.InfoHash), tracker.Port, bytesUploaded, bytesDownloaded, bytesLeft)
		if err == nil {
			return data, nil
		}
	}

	return bencode.Dictionary{}, errors.New("No announcer responded correctly")
}

// New returns a Tracker type with given parameters
func New(info *torrent_info.TorrentInfo, port int, peerId string) Tracker {
	tracker := Tracker{
		TorrentInfo: info,
		PeerId:      peerId,
		Port:        port,
	}
	return tracker
}
