package database

import (
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
	"time"
)

type Model struct {
	db      *sql.DB  // the database connection
	torrent string   // the torrent itself, used to identify before using integer ids
	id      int      // the id of the current torrent
	comm    chan int // channel for writing to the db
}

const (
	WRITE_INTERVAL = 3 * time.Second
)

func NewModel(torrent string) *Model {
	model := Model{
		torrent: torrent,
	}

	model.comm = make(chan int)
	go model.writerServer(model.comm)

	return &model
}

func (model *Model) Connect() error {
	var err error
	var cwd string
	if cwd, err = os.Getwd(); err != nil {
		return err
	}

	if model.db, err = sql.Open("sqlite3", filepath.Join(cwd, "database.db")); err != nil {
		model.db = nil
		return err
	}

	sqls := []string{
		"CREATE TABLE IF NOT EXISTS torrent_id (id INTEGER PRIMARY KEY AUTOINCREMENT, torrent VARCHAR(65536));",
		"CREATE UNIQUE INDEX IF NOT EXISTS torrent ON torrent_id(torrent);",
		"CREATE TABLE IF NOT EXISTS torrent_blocks (torrent_id INTEGER, block_id INTEGER);",
		"CREATE INDEX IF NOT EXISTS blocks_for_torrent ON torrent_blocks(torrent_id);",
		"CREATE TABLE IF NOT EXISTS torrent_time (torrent_id INTEGER PRIMARY KEY, time INTEGER DEFAULT 0);"}

	for _, sql := range sqls {
		if _, err := model.db.Exec(sql); err != nil {
			return err
		}
	}

	transaction, err := model.db.Begin()
	defer transaction.Commit()
	if err != nil {
		return err
	}

	row, err := transaction.Query("SELECT id FROM torrent_id WHERE torrent = ?", model.torrent)
	if err != nil {
		return err
	}
	defer row.Close()

	if row.Next() {
		if err := row.Scan(&model.id); err != nil {
			return err
		}

		return nil
	}

	result, err := transaction.Exec("INSERT INTO torrent_id(torrent) VALUES(?)", model.torrent)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	model.id = int(id)

	_, err = transaction.Exec("INSERT INTO torrent_time(torrent_id, time) VALUES(?, ?)", model.id, 0)
	if err != nil {
		return err
	}

	return nil
}

func (model *Model) writerServer(comm chan int) {
	blocks := make([]int, 0)
	ticker := time.NewTicker(WRITE_INTERVAL)
	for {
		select {
		case blockIndex, ok := <-comm:
			if !ok {
				return
			}
			blocks = append(blocks, blockIndex)
		case _ = <-ticker.C:
			model.db.Exec("BEGIN IMMEDIATE TRANSACTION")
			for _, blockIndex := range blocks {
				model.db.Exec("INSERT INTO torrent_blocks(torrent_id, block_id) VALUES(?, ?)", model.id, blockIndex)
			}
			model.db.Exec("COMMIT TRANSACTION")
			blocks = make([]int, 0)
		}
	}
}

func (model *Model) Disconnect() {
	close(model.comm)
	if model.db != nil {
		model.db.Close()
	}
}

func (model *Model) MarkBlockHave(blockIndex int) error {
	model.comm <- blockIndex
	return nil
}

func (model *Model) BlocksDownloaded() ([]int, error) {
	rows, err := model.db.Query("SELECT block_id FROM torrent_blocks WHERE torrent_id = ?", model.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	blocks := []int{}
	for rows.Next() {
		var block int
		if err := rows.Scan(&block); err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func (model *Model) BeginTransaction() error {
	if _, err := model.db.Exec("BEGIN TRANSACTION"); err != nil {
		return err
	}

	return nil
}

func (model *Model) CommitTransaction() error {
	if _, err := model.db.Exec("COMMIT TRANSACTION"); err != nil {
		return err
	}

	return nil
}

func (model *Model) AddTimeDownloaded(time int) error {
	if _, err := model.db.Exec("UPDATE torrent_time SET time = time + 2 WHERE torrent_id = ?", model.id); err != nil {
		return err
	}
	return nil
}

func (model *Model) TimeUsed() (int, error) {
	row, err := model.db.Query("SELECT time FROM torrent_time WHERE torrent_id = ?", model.id)
	if err != nil {
		return 0, err
	}
	defer row.Close()

	if row.Next() {
		var time int
		if err := row.Scan(&time); err != nil {
			return 0, err
		}
		return time, nil
	}
	return 0, errors.New("Torrent missing from database")
}
