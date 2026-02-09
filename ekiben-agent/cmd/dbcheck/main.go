package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"ekiben-agent/internal/db"
)

func main() {
	var dbPath string
	flag.StringVar(&dbPath, "db", "", "path to taiko.db3")
	flag.Parse()

	if dbPath == "" {
		log.Fatal("missing --db")
	}

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	count, err := db.CountUsers(sqlDB)
	if err != nil {
		log.Fatalf("count users: %v", err)
	}

	fmt.Fprintf(os.Stdout, "UserData rows: %d\n", count)
}
