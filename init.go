package main

import (
	"database/sql"
	"log"
	"os"
	"path"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/valyala/fasthttp"
)

func InitidCounter() {
	if err := DB.QueryRow("SELECT name FROM file ORDER BY name DESC LIMIT 1;").Scan(&IdCounter); err != nil {
		log.Fatal(err)
	}
}

func InitDB() {
	sqlitename := path.Join(WORKDIR, "app.sqlite")
	db, err := sql.Open("sqlite3", sqlitename)
	if err != nil {
		log.Fatal("打开数据库失败:", err)
	}

	// 2. 验证连接
	if err := db.Ping(); err != nil {
		log.Fatal("数据库连接失败:", err)
	}
	if _, err := db.Exec(CREATESQL, time.Now().Unix()); err != nil {
		if err.Error() != "UNIQUE constraint failed: file.id" {
			log.Fatal(err)
		}
	}

	DB = db
	log.Println("sqlite 初始化完成")
}
func InitLog() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	dir := path.Join(WORKDIR, "log.txt")

	var err error
	F, err = os.OpenFile(dir, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(F)
}

var F *os.File

func CloseLog() {
	if F != nil {
		F.Close()
	}
}

func InitRateLimiting() {
	for {
		LeakyBucket <- struct{}{}
		time.Sleep(100 * time.Millisecond)
	}
}

// LoadConfig 从指定路径加载配置文件
func InitEnv() {
	// 读取系统环境变量
	value := os.Getenv("GODISK_PORT")
	if value != "" {
		Port = value
	}
	value = os.Getenv("GODISK_ADMIN_KEY")
	if value == "" {
		log.Fatal("GODISK_ADMIN_KEY未设置")
	}
	AdminKey = value
	size, err := strconv.Atoi(os.Getenv("GODISK_UPLOAD_SIZE"))
	if err != nil {
		MaxUploadSize = 50
	} else {
		MaxUploadSize = size
	}

}

func InitdoWatchdogTaskDelfile() {
	var name string
	var isDir uint8
	var idd int
	var err error
	var row *sql.Rows
	defer func() {
		if row != nil {
			row.Close()
		}
	}()

	for i := range AsyncDel {
		err = DB.QueryRow("SELECT name,is_dir FROM file WHERE id = ? LIMIT 1", i).Scan(&name, &isDir)
		if err != nil {
			log.Println(err)
			err = nil
			continue
		}

		if isDir == 1 {
			_, err = DB.Exec(`DELETE FROM file WHERE id = ?`, i)
			if err != nil {
				log.Println(err)
				err = nil
				continue
			}
			row, err = DB.Query("SELECT id FROM file WHERE parent_id = ?", i)
			if err != nil {
				log.Println(err)
				err = nil
				continue
			}
			for row.Next() {
				err = row.Scan(&idd)
				if err != nil {
					log.Println(err)
					err = nil
					continue
				}
				AsyncDel <- idd
			}
		} else {
			_, err = DB.Exec(`UPDATE file_blob SET count = count - 1 WHERE name = ?;DELETE FROM file WHERE id = ?`, name, i)
			if err != nil {
				log.Println(err)
				err = nil
				continue
			}
		}
	}
}
func InitStaticFS() {
	fs := &fasthttp.FS{
		Root:               WORKDIR,
		IndexNames:         nil,
		GenerateIndexPages: false,
		Compress:           true,
		AcceptByteRange:    true,
	}
	StaticHandler = fs.NewRequestHandler()
}
