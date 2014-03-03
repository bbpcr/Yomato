package torrent_info

import (
	"bencode"
)

type SingleFileInfo struct {
	Name string
	Length int64
	Md5sum string
}

type InfoDictionary struct {
	
	RootPath string
	Files []SingleFileInfo
	MultipleFiles bool
	PieceLength int64
	Pieces string
	Private int64
}

type TorrentInfo struct {
	FileInformations InfoDictionary
	AnnounceUrl string
	AnnounceList []string
	CreationDate int64
	Comment string
	CreatedBy string
	Encoding string	
}

func getFilesInformationFromBencoder(decoded bencode.Bencoder , output *TorrentInfo) {
	
}

func getInfoDictionaryFromBencoder(decoded bencode.Bencoder , output *TorrentInfo) {
	dictionary := decoded.(bencode.Dictionary)
	
	for keys,values := range dictionary.Values {
	
		keyString := string((*keys).Value)
		switch keyString {
			case "piece length" :
				output.FileInformations.PieceLength = (*values).(bencode.Number).Value
			case "pieces" :
				output.FileInformations.Pieces = string((*values).(bencode.String).Value)
			case "private" :
				output.FileInformations.Private = (*values).(bencode.Number).Value
		}
	}
	
	output.FileInformations.MultipleFiles = false
	
	// Check if there are multiple files or not
	for keys,_ := range dictionary.Values {
	
		keyString := string((*keys).Value)
		if keyString == "files" {
			output.FileInformations.MultipleFiles = true
		}
	}
	
	if output.FileInformations.MultipleFiles {
		//We have two or more files
		
		for keys,values := range dictionary.Values {
	
			keyString := string((*keys).Value)
			switch keyString {
				case "name" : 
					output.FileInformations.RootPath = string((*values).(bencode.String).Value)
				default :
			}
		}
		
	} else {
		
		// We have only one file
		oneFile := SingleFileInfo{}
		for keys,values := range dictionary.Values {
	
			keyString := string((*keys).Value)
			switch keyString {
				case "name" : 
					output.FileInformations.RootPath = string((*values).(bencode.String).Value)
					oneFile.Name = output.FileInformations.RootPath
				case "length" :
					oneFile.Length = (*values).(bencode.Number).Value
				case "md5sum" :
					oneFile.Md5sum =  string((*values).(bencode.String).Value)
				default :
				
			}
		}
		
		output.FileInformations.Files = append(output.FileInformations.Files , oneFile)
	
	}
	
} 

func GetInfoFromBencoder(decoded bencode.Bencoder) TorrentInfo {
	
	info := TorrentInfo{}

	dictionary := decoded.(bencode.Dictionary)
	for keys,values := range dictionary.Values {
		keyString := string((*keys).Value)
		
		switch keyString {
			case "announce" :
				info.AnnounceUrl = string((*values).(bencode.String).Value)
			case "comment" : 
				info.Comment = string((*values).(bencode.String).Value)
			case "created by" : 
				info.CreatedBy = string((*values).(bencode.String).Value)
			case "creation date" : 
				info.CreationDate = (*values).(bencode.Number).Value
			case "encoding" : 
				info.Encoding = string((*values).(bencode.String).Value)
			case "announce-list" : 
			
				// It should be a list of list of strings
				announceListStrings := (*values).(bencode.List)
				for _,listBencoder := range announceListStrings.Values {
					
					realList := (*listBencoder).(bencode.List)
					
					for _,strings := range realList.Values {					
						realString := (*strings).(bencode.String)
						info.AnnounceList = append(info.AnnounceList , string(realString.Value))					
					}
				}
			case "info" :
				getInfoDictionaryFromBencoder(*values , &info)
			default : 
			
		}
	}
	return info
}

