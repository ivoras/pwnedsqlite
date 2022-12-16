package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/bodgit/sevenzip"
	_ "github.com/mattn/go-sqlite3"
)

var inFilename = flag.String("i", "", "Input file name (7zip file from https://haveibeenpwned.com/Passwords)")
var outFilename = flag.String("o", "pwned.sqlite", "Output filename SQLite database")
var forceNewDb = flag.Bool("f", true, "Delete old SQLite output file before ingesting new data")

func main() {
	flag.Parse()

	if *inFilename == "" {
		fmt.Println("Missing input file name (-i)")
		return
	}

	r, err := sevenzip.OpenReader(*inFilename)
	if err != nil {
		fmt.Println(err)
		return
	}

	if *forceNewDb {
		os.Remove(*outFilename)
		// ignore errors
	}

	db, err := sql.Open("sqlite3", *outFilename)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	if *forceNewDb {
		_, err = db.Exec("CREATE TABLE hashes (hash TEXT, count INTEGER);\nCREATE INDEX hashesh_hash ON hashes(hash);")
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	for _, file := range r.File {
		fmt.Println(file.Name)
		if strings.HasSuffix(file.Name, ".txt") {
			rc, err := file.Open()
			if err != nil {
				fmt.Println(err)
				return
			}
			err = ingestData(db, rc)
			rc.Close()
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

func ingestData(db *sql.DB, rc io.ReadCloser) error {
	var tx *sql.Tx
	var stmt *sql.Stmt

	beginTrans := func() (err error) {
		tx, err = db.Begin()
		if err != nil {
			return
		}
		stmt, err = tx.Prepare("INSERT INTO hashes(hash, count) VALUES (?, ?)")
		return
	}
	err := beginTrans()
	if err != nil {
		return err
	}

	rd := bufio.NewReaderSize(rc, 4*1024*1024)
	count := 0
	for {
		line, err := rd.ReadSlice(byte('\n'))
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		hash := line[0:40]
		countString := line[41 : len(line)-2]
		countInt, err := strconv.Atoi(string(countString))
		if err != nil {
			return err
		}
		// fmt.Println(string(hash), string(countString))
		_, err = stmt.Exec(hash, countInt)
		if err != nil {
			return err
		}
		count++
		if count%100000 == 0 {
			// Do it in batches
			err = tx.Commit()
			if err != nil {
				return err
			}
			err = beginTrans()
			if err != nil {
				return err
			}
			fmt.Print(".")
		}
	}
}
