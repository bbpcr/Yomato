package tracker

import (
	"errors"
	"fmt"
	"github.com/bbpcr/Yomato/bencode"
)

// GetPeers parses peers information from a given Bencoder and returns the same Bencoder dictionary
// but with a key ('peers') which holds a bencoded list, each element of list having a dictionary with
// keys 'port', 'ip', 'peer_id' for each peer
func GetPeers(bdecoded bencode.Bencoder) (bencode.Bencoder, error) {

	// Try to cast to dictionary
	responseDictionary, isDictionary := bdecoded.(*bencode.Dictionary)

	if !isDictionary {
		return bdecoded, errors.New("Malformed response!")
	}

	// Get the peer value
	peersBencoded := responseDictionary.Values[bencode.String{Value: "peers"}]

	// We have two posibilities , peersBencoded is a list of dictionaries or is a string
	// If it's a list , it's already decoded , if it's a string we need to decode it.
	stringPeers, isString := peersBencoded.(*bencode.String)
	_, isList := peersBencoded.(*bencode.List)

	if isString {

		// We have a binary form like this : multiple of 6 bytes , where the first 4 bytes are IP and the last 2 are the port number
		// We now create the list of dictionaries.
		bigList := new(bencode.List)

		byteArray := []byte(stringPeers.Value)
		for i := 0; i < len(byteArray); i += 6 {

			smallDictionary := new(bencode.Dictionary)
			smallDictionary.Values = make(map[bencode.String]bencode.Bencoder)

			numberPort := new(bencode.Number)
			numberPort.Value = int64(byteArray[i+4])<<8 + int64(byteArray[i+5])

			ip := new(bencode.String)
			ip.Value = fmt.Sprintf("%d.%d.%d.%d", byteArray[i], byteArray[i+1], byteArray[i+2], byteArray[i+3])

			peerId := new(bencode.String)
			peerId.Value = ""

			smallDictionary.Values[bencode.String{Value: "port"}] = numberPort
			smallDictionary.Values[bencode.String{Value: "ip"}] = ip
			smallDictionary.Values[bencode.String{Value: "peer id"}] = peerId
			bigList.Values = append(bigList.Values, smallDictionary)

		}
		responseDictionary.Values[bencode.String{Value: "peers"}] = bigList
	} else if !isList {

		// If it isn't a list or a string , it's something else , and we return an error.
		return bdecoded, errors.New("Malformed response!")
	}

	// We return the modified responseDictionary
	return responseDictionary, nil
}
