package main

import (
	"database/sql"
)

var DB *sql.DB

func CloseDB() {
	if DB == nil {
		return
	}
	DB.Close()
}
