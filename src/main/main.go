package main

import (
	"bencode"
	"fmt"
	"io/ioutil"
	"os"
	"torrent_info"
)

func main() {
	path := os.Args[1]
	data, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	res, _, err := bencode.Parse(data)
	if err != nil {
		panic(err)
	}
	fmt.Println("-----")
	
	torrentInfo := torrent_info.GetInfoFromBencoder(res)
	
	fmt.Println("Announce url : " , torrentInfo.AnnounceUrl)
	fmt.Println("Announce additional list : ", torrentInfo.AnnounceList)
	fmt.Println("Creation date : " , torrentInfo.CreationDate)
	fmt.Println("Comment : ", torrentInfo.Comment)
	fmt.Println("Created by : " , torrentInfo.CreatedBy)
	fmt.Println("Encoding : " , torrentInfo.Encoding)
	fmt.Println("Piece Length : ", torrentInfo.FileInformations.PieceLength)
	fmt.Println("Private : " , torrentInfo.FileInformations.Private)
	fmt.Println("More than two files? : ", torrentInfo.FileInformations.MultipleFiles)
	fmt.Println("File name / root name : " , torrentInfo.FileInformations.RootPath)
	fmt.Println()
	
	for index,fileInfo := range torrentInfo.FileInformations.Files {
		fmt.Println("	File #" , index , "-------")
		fmt.Println("	Name : " , fileInfo.Name)
		fmt.Println("	Size : " , fileInfo.Length)
		fmt.Println("	Md5sum (not always present) : ",fileInfo.Md5sum)
	}
	
	//res.Dump()
}
