package piece_manager

import (
	"errors"
	"sync"

	"github.com/bbpcr/Yomato/file_writer"
	"github.com/bbpcr/Yomato/peer"
	"github.com/bbpcr/Yomato/torrent_info"
)

const (
	BLOCK_LENGTH = 1 << 14
)

type PieceManager struct {
	blockBytes       []int  //tells me how much i need to download from a block [block:bytes]
	blockOffset      []int  //tells me the offset of the block in piece [block:pieceOffset]
	blockDownloading []bool //tells me if a block is downloading [block:true/false]
	blockPiece       []int  //tells me what piece the block belongs [block:piece]
	pieceBytes       []int  //tells me how much i downloaded from a piece [piece:bytes]
	pieceNumBlocks   []int  //tells me how many blocks a piece has until his position [piece:numBlocks]
	totalBlocks      int
	blocksLocker     sync.Mutex
	//These should be maps because, if a value doesnt exist then we dont download it.
}

func (manager *PieceManager) GetBlockIndex(pieceIndex int, offsetIndex int) int {

	//manager.blocksLocker.Lock()
	//defer manager.blocksLocker.Unlock()
	startPosition := manager.pieceNumBlocks[pieceIndex]
	howMany := offsetIndex / BLOCK_LENGTH
	startPosition += howMany
	return startPosition
}

func New(torrentInfo *torrent_info.TorrentInfo) *PieceManager {

	manager := &PieceManager{
		blockBytes:       make([]int , 0),
		blockOffset:      make([]int , 0),
		blockDownloading: make([]bool , 0),
		blockPiece:       make([]int , 0),
		pieceBytes:       make([]int , 0),
		pieceNumBlocks:   make([]int , 0),
	}

	blockIndex := 0

	for pieceIndex := 0; pieceIndex < int(torrentInfo.FileInformations.PieceCount); pieceIndex++ {

		manager.pieceNumBlocks = append(manager.pieceNumBlocks , blockIndex)
		pieceLength := torrentInfo.FileInformations.PieceLength
		if pieceIndex == int(torrentInfo.FileInformations.PieceCount)-1 {
			pieceLength = torrentInfo.FileInformations.TotalLength - torrentInfo.FileInformations.PieceLength*(torrentInfo.FileInformations.PieceCount-1)
		}
		numBlocks := pieceLength / BLOCK_LENGTH
		lastBlockSize := pieceLength % BLOCK_LENGTH
		offset := 0

		for blockPosition := 0; blockPosition < int(numBlocks); blockPosition++ {
			manager.blockBytes = append(manager.blockBytes , BLOCK_LENGTH)
			manager.blockDownloading = append(manager.blockDownloading, false)
			manager.blockPiece = append(manager.blockPiece, pieceIndex)
			manager.blockOffset = append(manager.blockOffset , offset)
			blockIndex++
			offset += BLOCK_LENGTH
		}

		manager.pieceBytes = append(manager.pieceBytes , 0)

		if lastBlockSize != 0 {
			manager.blockBytes = append(manager.blockBytes , int(lastBlockSize))
			manager.blockDownloading = append(manager.blockDownloading, false)
			manager.blockPiece = append(manager.blockPiece, pieceIndex)
			manager.blockOffset = append(manager.blockOffset , offset)
			blockIndex++
		}
	}
	manager.totalBlocks = blockIndex
	return manager
}

func (manager *PieceManager) AddPieceToDownload(pieceIndex int, torrentInfo *torrent_info.TorrentInfo) {
	manager.blocksLocker.Lock()
	defer manager.blocksLocker.Unlock()
	pieceLength := torrentInfo.FileInformations.PieceLength
	if pieceIndex == int(torrentInfo.FileInformations.PieceCount)-1 {
		pieceLength = torrentInfo.FileInformations.TotalLength - torrentInfo.FileInformations.PieceLength*(torrentInfo.FileInformations.PieceCount-1)
	}
	numBlocks := pieceLength / BLOCK_LENGTH
	lastBlockSize := pieceLength % BLOCK_LENGTH
	offset := 0
	blockIndex := manager.pieceNumBlocks[pieceIndex]

	for blockPosition := 0; blockPosition < int(numBlocks); blockPosition++ {
		manager.blockBytes[blockIndex] = BLOCK_LENGTH
		manager.blockDownloading[blockIndex] = false
		blockIndex++
		offset += BLOCK_LENGTH
	}

	manager.pieceBytes[pieceIndex] = 0

	if lastBlockSize != 0 {
		manager.blockBytes[blockIndex] = int(lastBlockSize)
		manager.blockDownloading[blockIndex] = false
		blockIndex++
	}
}

func (manager *PieceManager) RemovePieceFromDownload(pieceIndex int, torrentInfo *torrent_info.TorrentInfo) {
	manager.blocksLocker.Lock()
	defer manager.blocksLocker.Unlock()
	pieceLength := torrentInfo.FileInformations.PieceLength
	if pieceIndex == int(torrentInfo.FileInformations.PieceCount)-1 {
		pieceLength = torrentInfo.FileInformations.TotalLength - torrentInfo.FileInformations.PieceLength*(torrentInfo.FileInformations.PieceCount-1)
	}
	numBlocks := pieceLength / BLOCK_LENGTH
	lastBlockSize := pieceLength % BLOCK_LENGTH
	offset := 0
	blockIndex := manager.pieceNumBlocks[pieceIndex]

	for blockPosition := 0; blockPosition < int(numBlocks); blockPosition++ {
		manager.blockBytes[blockIndex] = 0
		manager.blockDownloading[blockIndex] = false
		blockIndex++
		offset += BLOCK_LENGTH
	}

	if lastBlockSize != 0 {
		manager.blockBytes[blockIndex] = 0
		manager.blockDownloading[blockIndex] = false
		blockIndex++
	}

	manager.pieceBytes[pieceIndex] = int(pieceLength)
}

// Returns the ID of the next piece to download.
// This can use multiple strategies, e.g.
// Sequentially (NOT good, easy for development)
// or randomized (much better)
func (manager *PieceManager) GetNextBlocksToDownload(for_peer *peer.Peer, maxBlocks int) []int {

	//manager.blocksLocker.Lock()
	//defer manager.blocksLocker.Unlock()
	blocks := []int{}
	for block, count := 0, 0; block < manager.totalBlocks && count < maxBlocks; block++ {
		if !manager.blockDownloading[block] && for_peer.BitfieldInfo.At(manager.blockPiece[block]) && manager.blockBytes[block] > 0 {
			blocks = append(blocks, block)
			count++
		}
	}

	if len(blocks) < maxBlocks {
		for block, count := 0, len(blocks); block < manager.totalBlocks && count < maxBlocks; block++ {
			if for_peer.BitfieldInfo.At(manager.blockPiece[block]) && manager.blockBytes[block] > 0 {
				blocks = append(blocks, block)
				count++
			}
		}
	}

	return blocks
}

func (manager *PieceManager) UpdatePiece(pieceData file_writer.PieceData) error {

	manager.blocksLocker.Lock()
	defer manager.blocksLocker.Unlock()
	blockIndex := manager.GetBlockIndex(pieceData.PieceNumber, pieceData.Offset)
	pieceLength := len(pieceData.Piece)
	if manager.blockBytes[blockIndex] > 0 && pieceLength > 0 {
		manager.blockBytes[blockIndex] -= pieceLength
		manager.pieceBytes[pieceData.PieceNumber] += pieceLength
	} else {
		return errors.New("Piece wasn't updated due to invalid params or piece already updated")
	}
	return nil
}

func (manager *PieceManager) SetPieceDownloading(pieceData file_writer.PieceData, value bool) {
	manager.blocksLocker.Lock()
	defer manager.blocksLocker.Unlock()
	blockIndex := manager.GetBlockIndex(pieceData.PieceNumber, pieceData.Offset)
	manager.blockDownloading[blockIndex] = value
}

func (manager *PieceManager) SetBlockDownloading(blockIndex int, value bool) {
	manager.blocksLocker.Lock()
	defer manager.blocksLocker.Unlock()
	manager.blockDownloading[blockIndex] = value
}

func (manager *PieceManager) MakeRequest(blockIndex int) (int, int, int) {

	pieceIndex := manager.blockPiece[blockIndex]
	pieceOffset := manager.blockOffset[blockIndex]
	pieceLength := manager.blockBytes[blockIndex]
	return pieceIndex, pieceOffset, pieceLength
}

func (manager *PieceManager) IsPieceCompleted(pieceIndex int, torrentInfo *torrent_info.TorrentInfo) bool {

	
	if pieceIndex == int(torrentInfo.FileInformations.PieceCount-1) {
		if torrentInfo.FileInformations.PieceCount >= 2 {
			lastPieceLength := torrentInfo.FileInformations.TotalLength - torrentInfo.FileInformations.PieceLength*(torrentInfo.FileInformations.PieceCount-1)
			if int64(manager.pieceBytes[pieceIndex]) >= lastPieceLength {
				return true
			}
		}

	} else if int64(manager.pieceBytes[pieceIndex]) >= torrentInfo.FileInformations.PieceLength {
		return true
	}
	return false
}

func (manager *PieceManager) CalculateDownloaded() int64 {
	var total int64 = 0
	for _, count := range manager.pieceBytes {
		total += int64(count)
	}
	return total
}
