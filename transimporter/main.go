package main

import (
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/petert82/go-translation-api/datastore"
	"os"
	"time"
)

func check(e error) {
	if e != nil {
		fmt.Println(e)
		os.Exit(1)
	}
}

func parseArgs(args []string) (dbPath string, importPath string, err error) {
	if len(args) < 2 {
		return "", "", errors.New("Usage:\n  transimporter DB_PATH IMPORT_PATH")
	}

	return args[0], args[1], nil
}

func main() {
	start := time.Now()
	dbFile, importPath, err := parseArgs(os.Args[1:])
	check(err)

	results := make(chan string, 100)
	done := make(chan bool, 1)

	go func() {
		for {
			imported := <-results
			fmt.Println("Imported domain: ", imported)
		}
	}()

	var (
		count int
		stats datastore.Stats
	)
	go func() {
		var db *sqlx.DB
		db, err = sqlx.Connect("sqlite3", dbFile)
		check(err)
		ds, err := datastore.New(db)
		check(err)
		count, err = ds.ImportDir(importPath, results)
		check(err)

		stats = ds.Stats

		done <- true
	}()
	<-done

	elapsed := time.Since(start).Seconds()
	fmt.Printf("Imported %v files in %fs\n\n", count, elapsed)

	fmt.Println(stats)
}
