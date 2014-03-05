package tracker

import (
	"errors"
	"fmt"
	"github.com/bbpcr/Yomato/bencode"
)

func GetPeers(bdecoded bencode.Bencoder) (bencode.Bencoder, error) {

	// Try to cast to dictionary
	responseDictionary, isDictionary := bdecoded.(*bencode.Dictionary)

	if !isDictionary {
		return bdecoded, errors.New("Malformed response!")
	}

	// Get the peer value

	peersBencoded := responseDictionary.Values[bencode.String{"peers"}]

	// We have two posibilities , peersBencoded is a list of dictionaries or is a string
	// If it's a list , it's already decoded , if it's a string we need to decode it.

	stringPeers, isString := peersBencoded.(*bencode.String)

	if isString {

		// We have a binary form like this : multiple of 6 bytes , where the first 4 bytes are IP and the last 2 are the port number
		// We now create the list of dictionaries.

		bigList := bencode.List{}

		byteArray := []byte(stringPeers.Value)
		for i := 0; i < len(byteArray); i += 6 {

			smallDictionary := bencode.Dictionary{}
			smallDictionary.Values = make(map[bencode.String]bencode.Bencoder)

			numberPort := bencode.Number{}
			numberPort.Value = int64(byteArray[i+4])<<8 + int64(byteArray[i+5])

			ip := bencode.String{}
			ip.Value = fmt.Sprintf("%d.%d.%d.%d", byteArray[i], byteArray[i+1], byteArray[i+2], byteArray[i+3])

			peerId := bencode.String{}

			smallDictionary.Values[bencode.String{"port"}] = numberPort
			smallDictionary.Values[bencode.String{"ip"}] = ip
			smallDictionary.Values[bencode.String{"peer id"}] = peerId
			bigList.Values = append(bigList.Values, smallDictionary)

		}
		responseDictionary.Values[bencode.String{"peers"}] = bigList
	} else {
		// We have the actual list and we do nothing to it
		// This list should have as a bonus the peer id.
	}

	//We return the modified responseDictionary

	return responseDictionary, nil
}
