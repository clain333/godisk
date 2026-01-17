package main

import (
	"database/sql"
	"log"
)

var DB *sql.DB

func CloseDB() {
	if DB == nil {
		return
	}

	err := DB.Close()
	if err != nil {
		log.Println("关闭数据库失败:", err)
	} else {
		log.Println("数据库已关闭")
	}

}
